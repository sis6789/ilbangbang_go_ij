package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	//"strconv"
	"strings"
	"sync"
	"time"
)

// GetContent get URL as byte array
func GetUrlContent(url string) ([]byte, error) {
	var result []byte
	var err error
	client := &http.Client{
		CheckRedirect: nil,
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", "curl/7.50.3")
	req.Header.Add("Accept", "*/*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	finalUrl := resp.Request.URL.String()
	if finalUrl != url {
		result, err = GetUrlContent(finalUrl)
	} else {
		defer resp.Body.Close()
		result, err = ioutil.ReadAll(resp.Body)
	}
	return result, err
}

// regex for name normalization
var chonly *regexp.Regexp = regexp.MustCompile("[^ 가-힣a-zA-Z0-9]")
var oneBlank *regexp.Regexp = regexp.MustCompile("\\s+")

var wgParseXML sync.WaitGroup

// for control of mp3 channel
type downReq struct {
	url    string
	folder string
	file   string
}

var wgFileRequest sync.WaitGroup
var chanFileRequest chan downReq = make(chan downReq)

// for XML Parser
type item struct {
	//XMLName     xml.Name `xml:"item"`
	Title       string `xml:"title"`
	Author      string `xml:"author"`
	Subtitle    string `xml:"subtitle"`
	Summary     string `xml:"summary"`
	Description string `xml:"description"`
	Enclosure   struct {
		Url    string `xml:"url,attr"`
		Length string `xml:"length,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"enclosure"`
	Guid     string `xml:"guid"`
	PubDate  string `xml:"pubDate"`
	Duration string `xml:"duration"`
	Explicit string `xml:"explicit"`
	Keywords string `xml:"keywords"`
}

type channel struct {
	//XMLName     xml.Name `xml:"channel"`
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Language    string `xml:"language"`
	Copyright   string `xml:"copyright"`
	Author      string `xml:"author"`
	Keywords    string `xml:"keywords"`
	Subtitle    string `xml:"subtitle"`
	Summary     string `xml:"summary"`
	Owner       struct {
		Email string `xml:"email"`
		Name  string `xml:"name"`
	} `xml:"owner"`
	Category string `xml:"category"`
	Image    struct {
		Href string `xml:"href,attr"`
		Text string
	} `xml:"image"`
	ImageHref string `xml:"href,attr"`
	Explicit  string `xml:"explicit"`
	Items     []item `xml:"item"`
}

type wholeBody struct {
	//XMLName xml.Name `xml:"rss"`
	Channel channel `xml:"channel"`
}

func urlFile(url string, folder string, filename string, wid int) {

	// check folder
	if _, err := os.Stat(folder); os.IsNotExist(err) {
		os.MkdirAll(folder, 0777)
	}

	// check file, if exist return immediately without action
	if fi, _ := os.Stat(folder + string(filepath.Separator) + filename); fi != nil {
		return
	}

	fmt.Println(wid, folder, filename)
	urlBytes, err := GetUrlContent(url)

	//open a file for writing
	file, err := os.Create(folder + string(filepath.Separator) + filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Use io.Copy to just dump the response body to the file. This supports huge files
	_, err = io.Copy(file, bytes.NewReader(urlBytes))
	if err != nil {
		log.Fatal(err)
	}

	log.Println(folder, filename)

}

func itemFilename(item item) (string, string) {
	// convert pub date
	t, _ := time.Parse(time.RFC1123Z, item.PubDate)
	//datetimeString := t.Format("060102-1504")
	dateString := t.Format("060102")
	/*
	// how long
	dursplit := strings.Split(item.Duration, ":")
	durH, _ := strconv.Atoi(dursplit[0])
	durM, _ := strconv.Atoi(dursplit[1])
	durS, _ := strconv.Atoi(dursplit[2])
	minutes := durH*60 + durM
	if durS > 0 {
		minutes += 1
	}
	minutesString := fmt.Sprintf("%03d", minutes)
	*/
	// file extension
	pointpos := strings.LastIndex(item.Enclosure.Url, ".")
	// normalize title
	editedTitle := string(oneBlank.ReplaceAll(chonly.ReplaceAll([]byte(strings.Trim(item.Title, " ")), []byte("")), []byte("_")))
	ext := string(item.Enclosure.Url[pointpos:])
	//return datetimeString + "-" + minutesString + "-" + editedTitle + ext, ext
	return dateString + "-" + editedTitle + ext, ext
}

func parseXml(url string) {

	defer wgParseXML.Done()

	// Defer randomly for random queuing
	randomDelayuration := time.Duration(rand.Int63n(3000)*int64(time.Millisecond) + 200)
	time.Sleep(randomDelayuration)

	urlBytes, _ := GetUrlContent(url)

	var wholeBody wholeBody
	xml.Unmarshal(urlBytes, &wholeBody)

	// folder name
	folderName := string(oneBlank.ReplaceAll(chonly.ReplaceAll([]byte(strings.Trim(wholeBody.Channel.Title, " ")), []byte("")), []byte("_")))

	//var mp3info item
	for _, mp3info := range wholeBody.Channel.Items {
		mp3Filename, ext := itemFilename(mp3info)
		if ext != ".mp3" {
			continue
		}

		//urlFile(mp3info.Enclosure.Url, folderName, mp3Filename)
		chanFileRequest <- downReq{mp3info.Enclosure.Url, folderName, mp3Filename}
	}

}

func main() {

	// Logging Setup
	logFile, err := os.OpenFile("download.log", os.O_RDWR | os.O_CREATE | os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Error opening log file: %v", err)
	}
	defer logFile.Close()

	log.SetOutput(logFile)
	log.Println("Begin: ", time.Now().Local())

	// activate downLoaders (goroutine)
	for ix := 0; ix < 6; ix++ {
		wgFileRequest.Add(1)
		go func(ix int) {
			wid := ix
			defer wgFileRequest.Done()
			for request := range chanFileRequest {
				// Terminate when no more data and channel is closed
				urlFile(request.url, request.folder, request.file, wid)
			}
		}(ix)
	}

	wgParseXML.Add(4)
	go parseXml("http://pod.ssenhosting.com/rss/tomato2u/ilbangbangspeaking.xml")
	go parseXml("http://pod.ssenhosting.com/rss/tomato2u/ilbangbang.xml")
	go parseXml("http://pod.ssenhosting.com/rss/tomato2u/ilbangbangch.xml")
	go parseXml("http://pod.ssenhosting.com/rss/tomato2u/ilbangbangabc.xml")
	wgParseXML.Wait()
	close(chanFileRequest)

	wgFileRequest.Wait()

	log.Println("End: ", time.Now().Local())

}
