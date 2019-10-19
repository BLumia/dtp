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
	"golang.org/x/net/proxy"
)

type typeCmdArgs struct {
	proxy      *string
	organize   *bool
	checkExist *bool
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

func parseArgs() typeCmdArgs {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] URL\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s https://twitter.com/doesnotmatter/status/978152342536077313\n", os.Args[0])
		fmt.Println("Options:")
		flag.PrintDefaults()
	}

	var cmdArgs typeCmdArgs

	cmdArgs.proxy = flag.String("p", "socks5://127.0.0.1:1080/", "a valid proxy url")
	cmdArgs.organize = flag.Bool("o", false, "self-organize downloaded file")
	cmdArgs.checkExist = flag.Bool("e", false, "check if resource existed to avoid re-download")

	flag.Parse()

	urlList := flag.Args()
	if len(urlList) == 0 {
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

func getSiteName(sourceURL *url.URL) (string, []string) {
	urlStr := sourceURL.String()

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

func getHTTPClient(cmdArgs typeCmdArgs) *http.Client {
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

func getTargetFilePath(siteName string, urlKeySegment []string, targetURL string) string {

	switch siteName {
	case "Twitter":
		if urlKeySegment != nil {
			userName := urlKeySegment[1]
			statusID := urlKeySegment[2]

			folders := []string{"Twitter", userName}
			fileName := fmt.Sprintf("%s_%s", statusID, path.Base(targetURL))

			pathStr := path.Join(folders...)
			fullPath := path.Join(pathStr, fileName)

			return fullPath
		}
	case "DeviantArt":
		if urlKeySegment != nil {
			userName := urlKeySegment[1]
			artID := urlKeySegment[2]

			folders := []string{"DeviantArt", userName}
			fileName := fmt.Sprintf("%s", artID)

			pathStr := path.Join(folders...)
			fullPath := path.Join(pathStr, fileName)

			return fullPath
		}
	}

	fmt.Println("Unsupported site:" + siteName)
	os.Exit(2)

	return "dtp_error_unsupported_url"
}

func downloadAndSave(siteName string, targetDownloadPath string, targetURL string, httpClient *http.Client, organize bool) {
	pathStr := path.Dir(targetDownloadPath)
	fileName := path.Base(targetDownloadPath)

	if organize {
		if _, err := os.Stat(pathStr); os.IsNotExist(err) {
			fmt.Printf("[%s] [mkdir] New folder for resources: %s\n", siteName, pathStr)
			err := os.MkdirAll(pathStr, 0755)
			if err != nil {
				fmt.Println("Error creating folder for store resource: " + fileName)
			}
		}
		fileName = targetDownloadPath
	}

	fmt.Printf("[%s] [download] [resource] %s ...\n", siteName, targetURL)
	if siteName == "Twitter" {
		targetURL = targetURL + ":orig"
	}
	resp, err := httpClient.Get(targetURL)
	if err != nil {
		fmt.Println("Error donwloading resource: " + targetURL)
		return
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
		return
	}

	fmt.Printf("[%s] [save] [resource] %s ...\n", siteName, fileName)

	io.Copy(out, resp.Body)
}

func checkExist(cmdArgs typeCmdArgs, siteName string, urlKeySegment []string, organize bool) {
	if !*cmdArgs.checkExist {
		return
	}

	fmt.Printf("[%s] [existence] existence check...\n", siteName)

	fakePath := getTargetFilePath(siteName, urlKeySegment, "*")

	if organize {
		fakePathDir := path.Dir(fakePath)

		if _, err := os.Stat(fakePathDir); os.IsNotExist(err) {
			return
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
		fmt.Printf("[%s] [existence] Resource(s) probably existed: %v\n", siteName, matchedFiles)
		os.Exit(0)
	}
}

func main() {
	fmt.Println("Running dtp rev 2")
	cmdArgs := parseArgs()

	singleURL := getSourceURL()
	siteName, urlKeySegment := getSiteName(singleURL)
	httpClient := getHTTPClient(cmdArgs)

	checkExist(cmdArgs, siteName, urlKeySegment, *cmdArgs.organize)
	matchedDownloadUrls := parseDOM(siteName, singleURL, httpClient)

	for _, targetURL := range matchedDownloadUrls {
		targetDownloadPath := getTargetFilePath(siteName, urlKeySegment, targetURL)
		downloadAndSave(siteName, targetDownloadPath, targetURL, httpClient, *cmdArgs.organize)
	}

	fmt.Println("Download finished!")
}
