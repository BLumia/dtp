// dtp project main.go

/*
in case we need to test a XPath
function getElementByXpath(path) {
  return document.evaluate(path, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue;
}
*/

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/antchfx/htmlquery"
	"github.com/pterm/pterm"
	"golang.org/x/net/proxy"
)

type typeCmdArgs struct {
	proxy          *string
	organize       *bool
	checkExistence *bool
	daemon         *bool
}

type Config struct {
	Proxy      string `json:"proxy"`
	WorkingDir string `json:"working_dir"`
}

func removeDuplicates(elements []string) []string {
	encountered := map[string]bool{}

	for v := range elements {
		encountered[elements[v]] = true
	}

	result := []string{}
	for key := range encountered {
		result = append(result, key)
	}
	return result
}

func parseArgs(cfg Config) typeCmdArgs {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] URL\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s https://twitter.com/doesnotmatter/status/978152342536077313\n", os.Args[0])
		fmt.Println("Options:")
		flag.PrintDefaults()
	}

	var cmdArgs typeCmdArgs

	cmdArgs.proxy = flag.String("p", cfg.Proxy, "a valid proxy url")
	cmdArgs.organize = flag.Bool("o", false, "self-organize downloaded file")
	cmdArgs.checkExistence = flag.Bool("e", false, "check if resource existed to avoid re-download")
	cmdArgs.daemon = flag.Bool("d", false, "start a http api server daemon at :1704")

	flag.Parse()

	urlList := flag.Args()
	if len(urlList) == 0 && !*cmdArgs.daemon {
		flag.Usage()
		os.Exit(2)
	}

	return cmdArgs
}

func getSourceURL() *url.URL {
	singleURL, err := url.ParseRequestURI(flag.Args()[0])
	if err != nil {
		fmt.Println("Invalid URL: " + flag.Args()[0])
		flag.Usage()
		os.Exit(2)
	}

	return singleURL
}

func getSiteNameFromUrlStr(urlStr string) (string, []string) {
	// Try Twitter
	{
		re := regexp.MustCompile(`twitter\.com\/(\w+)\/status\/(\d+)`)
		matchedResult := re.FindStringSubmatch(urlStr)

		if matchedResult != nil {
			return "Twitter", matchedResult
		}
	}

	// Try DeviantArt
	{
		re := regexp.MustCompile(`deviantart\.com\/([\w-]+)\/art\/([\w-]+)`)
		matchedResult := re.FindStringSubmatch(urlStr)

		if matchedResult != nil {
			return "DeviantArt", matchedResult
		}
	}

	return "Unknown", nil
}

func getSiteName(sourceURL *url.URL) (string, []string) {
	urlStr := sourceURL.String()
	return getSiteNameFromUrlStr(urlStr)
}

func (cmdArgs *typeCmdArgs) getHTTPClient() *http.Client {
	proxyURL, err := url.ParseRequestURI(*cmdArgs.proxy)
	if err != nil {
		fmt.Println("Invalid proxy: " + *cmdArgs.proxy)
		flag.Usage()
		os.Exit(2)
	}

	dialer, err := proxy.FromURL(proxyURL, proxy.Direct)
	//	dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:1080", nil, proxy.Direct)
	if err != nil {
		fmt.Println("Error creating dialer, proxy: " + *cmdArgs.proxy)
		os.Exit(2)
	}

	httpTransport := &http.Transport{}
	httpTransport.Dial = dialer.Dial

	cookieJar, _ := cookiejar.New(nil)

	httpClient := &http.Client{
		Jar:       cookieJar,
		Transport: httpTransport,
	}

	return httpClient
}

func parseDeviantArtDomByXPath(htmlDom []byte) []string {
	doc, _ := htmlquery.Parse(bytes.NewReader(htmlDom))
	list := htmlquery.Find(doc, "//div[@data-hook='art_stage']//img")
	var matchedResult []string
	for _, oneElement := range list {
		matchedResult = append(matchedResult, htmlquery.SelectAttr(oneElement, "src"))
	}
	matchedResult = removeDuplicates(matchedResult)

	return matchedResult
}

func parseDeviantArtDomByRegexFallback(htmlDom []byte) []string {
	re := regexp.MustCompile(`"(http[s]?:\/\/www\.deviantart\.com\/download\/\w+\/[\w-]+\.(?:jpg|png)[\w=?;&]+)"`)
	matchedResult := removeDuplicates(re.FindAllString(string(htmlDom), -1))

	return matchedResult
}

func parseTwitterDomByXPath(htmlDom []byte) []string {
	doc, _ := htmlquery.Parse(bytes.NewReader(htmlDom))
	list := htmlquery.Find(doc, "//div[contains(@class,'permalink-tweet')][1]//div[contains(@class,'AdaptiveMedia-photoContainer')]//img/@src")
	var matchedResult []string
	for _, oneElement := range list {
		matchedResult = append(matchedResult, htmlquery.SelectAttr(oneElement, "src"))
	}
	matchedResult = removeDuplicates(matchedResult)

	return matchedResult
}

func parseTwitterDomByRegex(htmlDom []byte) []string {
	re := regexp.MustCompile(`http[s]?:\/\/pbs\.twimg\.com\/media\/\w+\.(?:jpg|png)`)
	matchedResult := removeDuplicates(re.FindAllString(string(htmlDom), -1))

	return matchedResult
}

func parseDOM(siteName string, url *url.URL, client *http.Client) []string {
	switch siteName {
	case "Twitter":
		return parseTwitterDOM(url, client)
	case "DeviantArt":
		return parseDeviantArtDOM(url, client)
	}

	return nil
}

func getDomStr(url *url.URL, client *http.Client) []byte {
	resp, err := client.Get(url.String())
	if err != nil {
		fmt.Println("[Panic] Error getting web page: " + url.String())
		os.Exit(2)
	}
	defer resp.Body.Close()

	htmlDomStr, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("[Panic] Error reading web page: " + url.String())
		os.Exit(2)
	}

	return htmlDomStr
}

func parseDeviantArtDOM(url *url.URL, client *http.Client) []string {
	fmt.Println("[DeviantArt] [fetch] Downloading web page...")

	htmlDomStr := getDomStr(url, client)

	fmt.Println("[DeviantArt] [match] Parsing web page...")
	matchedResult := parseDeviantArtDomByXPath(htmlDomStr)

	if len(matchedResult) == 0 {
		fmt.Println("[DeviantArt] [match] Download button match failed, will do another try...")
		matchedResult = parseDeviantArtDomByRegexFallback(htmlDomStr)
	}

	if len(matchedResult) == 0 {
		fmt.Println("[DeviantArt] [match] Still cannot find download link, quit...")
		os.Exit(0)
	}

	fmt.Printf("[DeviantArt] [match] resource(s): %v\n", matchedResult)
	return matchedResult
}

func parseTwitterDOM(url *url.URL, client *http.Client) []string {
	fmt.Println("[Twitter] [fetch] Downloading web page...")

	htmlDomStr := getDomStr(url, client)

	fmt.Println("[Twitter] [match] Parsing web page...")
	matchedResult := parseTwitterDomByXPath(htmlDomStr)

	if len(matchedResult) == 0 {
		fmt.Println("XPath match didn't get any result, fallback to RegEx match...")
		matchedResult = parseTwitterDomByRegex(htmlDomStr)
	}

	if len(matchedResult) == 0 {
		fmt.Println("No resource(s) found on page: " + url.String())
		os.Exit(0)
	}

	fmt.Printf("[Twitter] [match] resource(s): %v\n", matchedResult)
	return matchedResult
}

func stripQueryParam(inURL string) string {
	u, err := url.Parse(inURL)
	if err != nil {
		// TODO: log or handle error, in the meanwhile just return the original
		return inURL
	}
	u.RawQuery = ""
	return u.String()
}

func getTargetFilePath(siteName string, urlKeySegment []string, targetURL string) (string, string) {

	switch siteName {
	case "Twitter":
		if urlKeySegment != nil {
			userName := urlKeySegment[1]
			statusID := urlKeySegment[2]

			folders := []string{"Twitter", userName}
			fileName := fmt.Sprintf("%s_%s", statusID, path.Base(stripQueryParam(targetURL)))

			pathStr := path.Join(folders...)
			fullPath := path.Join(pathStr, fileName)

			return fullPath, statusID
		}
	case "DeviantArt":
		if urlKeySegment != nil {
			userName := urlKeySegment[1]
			artID := urlKeySegment[2]

			folders := []string{"DeviantArt", userName}
			fileName := fmt.Sprintf("%s", artID)

			pathStr := path.Join(folders...)
			fullPath := path.Join(pathStr, fileName)

			return fullPath, artID
		}
	}

	fmt.Println("Unsupported site:" + siteName)
	os.Exit(2)

	return "dtp_error_unsupported_url", "Unknown"
}

func downloadAndSave(siteName string, targetDownloadPath string, targetURL string, httpClient *http.Client, organize bool,
	spinner *pterm.SpinnerPrinter) bool {

	pathStr := path.Dir(targetDownloadPath)
	fileName := path.Base(targetDownloadPath)

	if organize {
		if _, err := os.Stat(pathStr); os.IsNotExist(err) {
			spinner.UpdateText(fmt.Sprintf("[%s] [mkdir] New folder for resources: %s", siteName, pathStr))
			err := os.MkdirAll(pathStr, 0755)
			if err != nil {
				fmt.Println("Error creating folder for store resource: " + fileName)
			}
		}
		fileName = targetDownloadPath
	}

	spinner.UpdateText(fmt.Sprintf("[%s] [download] [resource] %s ...", siteName, targetURL))
	// no longer doing this...
	// if siteName == "Twitter" {
	// 	targetURL = targetURL + ":orig"
	// }
	retry := 5
Label_retry:
	resp, err := httpClient.Get(targetURL)
	if err != nil {
		spinner.UpdateText(fmt.Sprintln("Error donwloading resource: " + targetURL + ", retry..."))
		if retry > 0 {
			retry--
			goto Label_retry
		} else {
			return false
		}
	}
	defer resp.Body.Close()

	// if no suffix in fileName, add suffix by reading response header.
	if !strings.ContainsAny(fileName, ".") {
		dispositionStr := resp.Header.Get("Content-Disposition")
		_, params, err := mime.ParseMediaType(dispositionStr)
		if err == nil {
			dispositionFilename := params["filename"]
			suffix := filepath.Ext(dispositionFilename)
			fileName = fileName + suffix
		}
	}

	// still no suffix in fileName? try from content-type
	if !strings.ContainsAny(fileName, ".") {
		contentTypeStr := resp.Header.Get("Content-Type")
		exts, err := mime.ExtensionsByType(contentTypeStr)
		if err == nil && len(exts) > 0 {
			suffix := exts[0]
			fileName = fileName + suffix
		}
	}

	out, err := os.Create(fileName)
	if err != nil {
		fmt.Println("Error creating file for downloaded resource: " + fileName)
		return false
	}

	spinner.UpdateText(fmt.Sprintf("[%s] [save] [resource] %s ...", siteName, fileName))

	io.Copy(out, resp.Body)

	spinner.UpdateText(fmt.Sprintf("[%s] [sync] [resource] %s ...", siteName, fileName))

	err = out.Sync()
	if err != nil {
		fmt.Printf("Error when sync file to disk: %s", err)
		return false
	}
	out.Close()

	return true
}

func (cmdArgs *typeCmdArgs) checkExist(siteName string, urlKeySegment []string, organize bool,
	spinner *pterm.SpinnerPrinter) (bool, string) {

	if !*cmdArgs.checkExistence {
		return false, ""
	}

	spinner.UpdateText(fmt.Sprintf("[%s] [existence] existence check...", siteName))

	fakePath, artId := getTargetFilePath(siteName, urlKeySegment, "*")

	if organize {
		fakePathDir := path.Dir(fakePath)

		if _, err := os.Stat(fakePathDir); os.IsNotExist(err) {
			return false, artId
		}
	} else {
		fakePath = path.Base(fakePath)
	}

	if !strings.ContainsAny(fakePath, ".") {
		fakePath = fakePath + ".*"
	}

	matchedFiles, err := filepath.Glob(fakePath)
	if err != nil {
		fmt.Println("Error checking exist, maybe match pattern is not correct: " + fakePath)
	}
	if len(matchedFiles) != 0 {
		pterm.Debug.Println(fmt.Sprintf("[%s] [existence] Resource(s) probably existed: %v", siteName, matchedFiles))
		return true, artId
	}

	return false, artId
}

func (cmdArgs *typeCmdArgs) apiUrlList(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		spinnerInfo, _ := pterm.DefaultSpinner.Start("[API] [start] received one request!")
		r.ParseMultipartForm(32 << 20)
		var matchedDownloadUrls []string
		_ = json.Unmarshal([]byte(r.PostFormValue("urlList")), &matchedDownloadUrls)

		singleURL := r.PostFormValue("source")
		siteName, urlKeySegment := getSiteNameFromUrlStr(singleURL)
		httpClient := cmdArgs.getHTTPClient()

		statusStr := "Failed, source: " + singleURL
		artId := ""
		defer func(statusStr *string) {
			finalMsg := fmt.Sprintf("[%s] [%s] Final Status: %s", siteName, artId, *statusStr)
			if strings.Contains(*statusStr, "Error when saving") {
				spinnerInfo.Fail(finalMsg)
			} else if strings.Contains(*statusStr, "Well done") {
				spinnerInfo.Success(finalMsg)
			} else {
				spinnerInfo.Info(finalMsg)
			}
		}(&statusStr)

		existed, _artId := cmdArgs.checkExist(siteName, urlKeySegment, *cmdArgs.organize, spinnerInfo)
		if existed {
			statusStr = "Existed!"
			artId = _artId
			return
		}

		for _, targetURL := range matchedDownloadUrls {
			targetDownloadPath, artId2 := getTargetFilePath(siteName, urlKeySegment, targetURL)
			artId = artId2
			succ := downloadAndSave(siteName, targetDownloadPath, targetURL, httpClient, *cmdArgs.organize, spinnerInfo)
			if !succ {
				statusStr = "Error when saving!" + singleURL
				return
			}
		}

		statusStr = "Well done!"
	} else {
		fmt.Fprintf(w, string("403"))
	}
}

func main() {
	// pterm.EnableDebugMessages()
	pterm.Info.Println("Running dtp rev 7")

	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}

	cfg := Config{
		Proxy: "socks5://127.0.0.1:1080/",
	}

	configFilePath := path.Join(filepath.Dir(ex), "config.json")
	jsonFile, err := os.Open(configFilePath)
	if err == nil {
		defer jsonFile.Close()
		byteValue, _ := ioutil.ReadAll(jsonFile)
		json.Unmarshal(byteValue, &cfg)
		if len(cfg.WorkingDir) > 0 {
			err = os.Chdir(cfg.WorkingDir)
			if err != nil {
				panic(err)
			}
			pterm.Info.Println("Working Dir: " + cfg.WorkingDir)
		}
	}

	cmdArgs := parseArgs(cfg)

	if *cmdArgs.daemon {
		http.HandleFunc("/urlList", cmdArgs.apiUrlList)
		err := http.ListenAndServe(":1704", nil)
		if err != nil {
			fmt.Println(err)
		}
	} else {
		singleURL := getSourceURL()
		siteName, urlKeySegment := getSiteName(singleURL)
		httpClient := cmdArgs.getHTTPClient()

		spinnerInfo, _ := pterm.DefaultSpinner.Start("[start] received one request!")
		existed, _ := cmdArgs.checkExist(siteName, urlKeySegment, *cmdArgs.organize, spinnerInfo)
		if existed {
			os.Exit(0)
		}
		matchedDownloadUrls := parseDOM(siteName, singleURL, httpClient)

		for _, targetURL := range matchedDownloadUrls {
			targetDownloadPath, _ := getTargetFilePath(siteName, urlKeySegment, targetURL)
			downloadAndSave(siteName, targetDownloadPath, targetURL, httpClient, *cmdArgs.organize, spinnerInfo)
		}

		fmt.Println("Download finished!")
	}
}
