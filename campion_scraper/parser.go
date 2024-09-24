package campionscraper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"go.uber.org/zap"
	"golang.org/x/net/html"
	"vasiluta.ro/ia_kn_stats/scraper"
)

var location *time.Location

var replacements = map[string]string{
	"dec": "December",
	"nov": "November",
	"oct": "October",
	"sep": "September",
	"aug": "August",
	"iul": "July",
	"iun": "June",
	"mai": "May",
	"apr": "April",
	"mar": "March",
	"feb": "February",
	"ian": "January",
}

var _ scraper.Parser[int] = &CampionParser{}

const subsPerPage = 14
const campionFormat = "_2 January 2006, 15:04"

func parseSubmission(node *html.Node) (*scraper.Submission, error) {
	sel := goquery.NewDocumentFromNode(node)
	//zap.S().Warn(node.)

	var sub = new(scraper.Submission)
	idText := strings.TrimSpace(goquery.NewDocumentFromNode(sel.Children().Nodes[0]).Text())
	id, err := strconv.Atoi(strings.TrimPrefix(idText, "#"))
	if err != nil {
		zap.S().Warn("Invalid ID from ", idText)
		return nil, err
	}
	sub.ID = id
	sub.Handled = true

	sub.Username = strings.TrimSpace(goquery.NewDocumentFromNode(sel.Children().Nodes[2]).Find("a").First().Text())
	sub.DisplayName = strings.TrimSpace(goquery.NewDocumentFromNode(sel.Children().Nodes[1]).Find("a").First().Text())

	pbName := strings.TrimSpace(goquery.NewDocumentFromNode(sel.Children().Nodes[3]).Text())
	sub.ProblemName = &pbName
	pbHref := strings.TrimPrefix(goquery.NewDocumentFromNode(sel.Children().Nodes[3]).Find("a").AttrOr("href", ""), "index.php?page=problem&action=view&id=")
	sub.ProblemID = &pbHref

	date := strings.TrimSpace(sel.Children().Nodes[6].FirstChild.Data)
	for k, v := range replacements {
		date = strings.ReplaceAll(date, k, v)
	}
	t, err := time.ParseInLocation(campionFormat, date, location)
	if err != nil {
		zap.S().Info("Invalid time from campion.edu.ro: ", date)
		return nil, errors.New("invalid time")
	}
	sub.Date = t

	score := strings.TrimSpace(goquery.NewDocumentFromNode(sel.Children().Nodes[7]).Find("a").First().Text())
	val, err := strconv.Atoi(score)
	if err != nil {
		zap.S().Info(score)
	} else {
		sub.Score = &val
	}

	return sub, nil
}

func ParseMonitorPage(ctx context.Context, offset int) ([]*scraper.Submission, error) {
	page := offset/subsPerPage + 1
	url := url.URL{
		Scheme:   "http",
		Host:     "campion.edu.ro",
		Path:     "arhiva/index.php",
		RawQuery: fmt.Sprintf("page=sources&action=view&paging=%d", page),
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	sel := doc.Find(".loctabel")
	var subs = make([]*scraper.Submission, 0, subsPerPage+5)
	for _, node := range sel.Find(`tr[onmouseover]:not([onmouseover=""])`).Children().Nodes {
		sub, err := parseSubmission(node)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

type CampionParser struct{}

func (p *CampionParser) PageZeroOffset() int {
	return 0
}

func (p *CampionParser) FurthestOffset(ctx context.Context, db *scraper.DB) (int, error) {
	return db.CountSubmissions(ctx)
}

func (p *CampionParser) NextPageOffset(t int, subs []*scraper.Submission) int {
	return t + len(subs)
}

func (p *CampionParser) GetPage(ctx context.Context, offset int) ([]*scraper.Submission, error) {
	return ParseMonitorPage(ctx, offset)
}

func init() {
	loc, err := time.LoadLocation("Europe/Bucharest")
	if err != nil {
		panic(err)
	}
	location = loc
}
