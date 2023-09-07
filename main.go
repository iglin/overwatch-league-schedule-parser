package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	prefix = "<script id=\"__NEXT_DATA__\" type=\"application/json\">"
	suffix = "</script>"
)

var (
	laLocation     *time.Location
	almatyLocation *time.Location
)

func init() {
	var err error
	laLocation, err = time.LoadLocation("America/Los_Angeles")
	if err != nil {
		panic(err)
	}
	almatyLocation, err = time.LoadLocation("Asia/Almaty")
	if err != nil {
		panic(err)
	}
}

func main() {
	bodyString := getPageAsString()

	translations := parsePage(bodyString)

	res := make([]TypedTranslation, 0, len(translations))
	for _, tr := range translations {
		res = append(res, tr.ToTypedTranslation())
	}

	sort.SliceStable(res, func(i, j int) bool {
		return res[i].AlmatyTime.Before(res[j].AlmatyTime)
	})

	reportJson(res)
	reportCsv(res)
}

func parsePage(bodyString string) []Translation {
	prefixIdx := strings.Index(bodyString, prefix)
	bodyString = bodyString[prefixIdx+len(prefix):]
	suffixIdx := strings.Index(bodyString, suffix)
	jsonString := bodyString[:suffixIdx]

	var jsonMap map[string]any
	if err := json.Unmarshal([]byte(jsonString), &jsonMap); err != nil {
		panic(err)
	}

	blocks := getSlice(getMap(jsonMap, "props", "pageProps"), "blocks")
	var tabsSlice []any
	for _, block := range blocks {
		if tabs, ok := block.(map[string]any)["tabs"]; ok {
			tabsSlice = getSlice(tabs.(map[string]any), "tabs")
			break
		}
	}

	translations := make([]Translation, 0)

	for _, tab := range tabsSlice {
		blocks := tab.(map[string]any)["blocks"].([]any)
		for _, block := range blocks {
			articleRaw := getString(block.(map[string]any), "richTextEditor", "articleRawHtml")
			translations = append(translations, parseArticleRawHtml(articleRaw)...)
		}
	}

	return translations
}

func getPageAsString() string {
	resp, err := http.DefaultClient.Get("https://overwatchleague.com/en-us/pathtopro/schedule")
	if err != nil {
		panic(err)
	}
	if resp.StatusCode != 200 {
		panic("response status: " + fmt.Sprintf("%d", resp.StatusCode))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	return string(bodyBytes)
}

func parseArticleRawHtml(articleRaw string) []Translation {
	articleRaw = strings.ReplaceAll(articleRaw, "&", "amp;")
	var htmlTable HtmlTable
	err := xml.Unmarshal([]byte(articleRaw), &htmlTable)
	if err != nil {
		panic(err)
	}

	res := make([]Translation, 0, len(htmlTable.TBody.Tr))
	for _, tr := range htmlTable.TBody.Tr {
		res = append(res, tr.ToTranslation())
	}
	return res
}

func reportJson(res []TypedTranslation) {
	jsonBytes, err := json.Marshal(res)
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile("overwatch-translations.json", jsonBytes, 0777); err != nil {
		panic(err)
	}
}

func reportCsv(res []TypedTranslation) {
	csv := "Almaty Time,Tournament,Region,Broadcast,Original Time,Original Date\r\n"
	for _, translation := range res {
		csv += fmt.Sprintf("%v,%s,%s,%s,%v,%s\r\n",
			translation.AlmatyTime.Format("02 Jan 06 15:04 MST"),
			translation.Tournament,
			translation.Region,
			translation.Broadcast,
			translation.OriginalTime.Format("02 Jan 06 15:04 MST"),
			translation.OriginalDate,
		)
	}
	if err := os.WriteFile("overwatch-translations.csv", []byte(csv), 0777); err != nil {
		panic(err)
	}
}

type TypedTranslation struct {
	AlmatyTime   time.Time `json:"almatyTime"`
	Tournament   string    `json:"tournament"`
	Region       string    `json:"region"`
	Broadcast    string    `json:"broadcast"`
	OriginalTime time.Time `json:"originalTime"`
	OriginalDate string    `json:"originalDate"`
}

type Translation struct {
	Date       string
	Tournament string
	Region     string
	Time       string
	Broadcast  string
}

func (t Translation) ToTypedTranslation() TypedTranslation {
	var timeVal time.Time
	var err error
	if strings.HasSuffix(t.Time, "PT") {
		timeVal, err = time.ParseInLocation("01-02-2006 3:04 PM", t.Date+" "+t.Time[:len(t.Time)-3], laLocation)
		if err != nil {
			panic(err)
		}
	} else {
		timeVal, err = time.Parse("01-02-2006 3:04 PM MST", t.Date+" "+t.Time)
		if err != nil {
			panic(err)
		}
	}
	return TypedTranslation{
		OriginalDate: t.Date + " " + t.Time,
		Tournament:   t.Tournament,
		Region:       t.Region,
		AlmatyTime:   timeVal.In(almatyLocation),
		OriginalTime: timeVal,
		Broadcast:    t.Broadcast,
	}
}

type HtmlTable struct {
	XMLName xml.Name `xml:"table"`
	THead   any      `xml:"thead"`
	TBody   TBody    `xml:"tbody"`
}

type TBody struct {
	Tr []Tr `xml:"tr"`
}

type Tr struct {
	Td []Td `xml:"td"`
}

func (tr Tr) GetField(name string) string {
	for _, td := range tr.Td {
		if td.Key == name {
			return td.Value
		}
	}
	return ""
}

func (tr Tr) ToTranslation() Translation {
	return Translation{
		Date:       tr.GetField("dateBody"),
		Tournament: tr.GetField("tournamentBody"),
		Region:     tr.GetField("regionBody"),
		Time:       tr.GetField("timeBody"),
		Broadcast:  tr.GetField("broadcastBody"),
	}
}

type Td struct {
	Key   string `xml:"class,attr"`
	Value string `xml:",chardata"`
}

func getMap(jsonMap map[string]any, path ...string) map[string]any {
	if len(path) == 1 {
		return jsonMap[path[0]].(map[string]any)
	}
	return getMap(jsonMap[path[0]].(map[string]any), path[1:]...)
}

func getString(jsonMap map[string]any, path ...string) string {
	if len(path) == 1 {
		return jsonMap[path[0]].(string)
	}
	return getString(jsonMap[path[0]].(map[string]any), path[1:]...)
}

func getSlice(jsonMap map[string]any, path string) []any {
	return jsonMap[path].([]any)
}
