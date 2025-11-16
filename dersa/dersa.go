package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// parseInt safely converts string → int
func parseInt(s string) int {
	if num, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return num
	}
	return 0
}

// fetchRouteInfo scrapes one route and returns the report as a string
func fetchRouteInfo(routeID string) (string, error) {
	res, err := http.Get("https://semil.sp.gov.br/travessias/")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return "", err
	}

	fromLocation := doc.Find(fmt.Sprintf("#menu-travessia-a-%s", routeID)).Text()
	toLocation := doc.Find(fmt.Sprintf("#menu-travessia-b-%s", routeID)).Text()
	timeFrom := parseInt(doc.Find(fmt.Sprintf("#menu-travMinutosA-%s", routeID)).Text())
	timeTo := parseInt(doc.Find(fmt.Sprintf("#menu-travMinutosB-%s", routeID)).Text())
	vessels := parseInt(doc.Find(fmt.Sprintf("#menu-embarcacao-%s strong", routeID)).Text())
	conditions := doc.Find(fmt.Sprintf("#menu-tempoClima-%s", routeID)).AttrOr("title", "Unknown")
	routeTitle := doc.Find(fmt.Sprintf("#menu-title-%s", routeID)).AttrOr("title", "Unknown")

	// Build a string instead of printing
	report := fmt.Sprintf(
		`=== DIRECT ID EXTRACTION ===
Route: %s
%s → %s: %d minutes
%s → %s: %d minutes
Total time: %d minutes
Vessels: %d
Weather: %s
Updated: %s`,
		routeTitle,
		fromLocation, toLocation, timeFrom,
		toLocation, fromLocation, timeTo,
		timeFrom+timeTo,
		vessels,
		conditions,
		time.Now().Format("15:04:05"),
	)

	return report, nil
}

func main() {
	routeID := "1951"
	report, err := fetchRouteInfo(routeID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(report)
}
