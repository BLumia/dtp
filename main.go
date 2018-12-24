// dtp project main.go
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"

	"github.com/antchfx/htmlquery"
	"golang.org/x/net/proxy"
)

type typeCmdArgs struct {
	proxy    *string
	organize *bool
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
	httpClient := &http.Client{Transport: httpTransport}

	return httpClient
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
	re := regexp.MustCompile("http[s]?:\\/\\/pbs\\.twimg\\.com\\/media\\/\\w+\\.(?:jpg|png)")
	matchedResult := removeDuplicates(re.FindAllString(string(htmlDom), -1))

	return matchedResult
}

func parseDOM(url *url.URL, client *http.Client) []string {
	fmt.Println("Downloading web page...")

	resp, err := client.Get(url.String())
	if err != nil {
		fmt.Println("Error getting web page: " + url.String())
		os.Exit(2)
	}
	defer resp.Body.Close()

	htmlDomStr, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading web page: " + url.String())
		os.Exit(2)
	}

	fmt.Println("Parsing web page...")
	matchedResult := parseTwitterDomByXPath(htmlDomStr)

	if len(matchedResult) == 0 {
		fmt.Println("XPath match didn't get any result, fallback to RegEx match...")
		matchedResult = parseTwitterDomByRegex(htmlDomStr)
	}

	if len(matchedResult) == 0 {
		fmt.Println("No resource(s) found on page: " + url.String())
		os.Exit(0)
	}

	fmt.Printf("Matched resource(s): %v\n", matchedResult)
	return matchedResult
}

func getTargetFolderAndFileName(sourceURL *url.URL, targetURL string) string {
	urlStr := sourceURL.String()

	re := regexp.MustCompile("twitter\\.com\\/(\\w+)\\/status\\/(\\d+)")
	matchedResult := re.FindStringSubmatch(urlStr)

	userName := matchedResult[1]
	statusID := matchedResult[2]

	folders := []string{"Twitter", userName}
	fileName := fmt.Sprintf("%s_%s", statusID, path.Base(targetURL))

	pathStr := path.Join(folders...)
	fullPath := path.Join(pathStr, fileName)

	return fullPath
}

func downloadAndSave(targetDownloadPath string, targetURL string, httpClient *http.Client, organize bool) {
	pathStr := path.Dir(targetDownloadPath)
	fileName := path.Base(targetDownloadPath)

	if organize {
		if _, err := os.Stat(pathStr); os.IsNotExist(err) {
			fmt.Println("Creating new folder for store resources: " + pathStr)
			err := os.MkdirAll(pathStr, 0755)
			if err != nil {
				fmt.Println("Error creating folder for store resource: " + fileName)
			}
		}
		fileName = targetDownloadPath
	}

	out, err := os.Create(fileName)
	if err != nil {
		fmt.Println("Error creating file for downloaded resource: " + fileName)
		return
	}

	fmt.Printf("Downloading resource %s as %s ...\n", targetURL, fileName)
	resp, err := httpClient.Get(targetURL + ":orig")
	if err != nil {
		fmt.Println("Error donwloading resource: " + targetURL)
		return
	}
	defer resp.Body.Close()

	io.Copy(out, resp.Body)
}

func main() {
	cmdArgs := parseArgs()

	singleURL := getSourceURL()
	httpClient := getHTTPClient(cmdArgs)

	matchedResult := parseDOM(singleURL, httpClient)

	for _, targetURL := range matchedResult {
		targetDownloadPath := getTargetFolderAndFileName(singleURL, targetURL)
		downloadAndSave(targetDownloadPath, targetURL, httpClient, *cmdArgs.organize)
	}

	fmt.Println("Download finished!")
}
