package ia_scraper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"go.uber.org/zap"
	"golang.org/x/net/html"
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

// 13 September 22 00:51:27
const iaFormat = "_2 January 06 15:04:05"

func parseSubmission(node *html.Node) (*IASubmission, error) {
	sel := goquery.NewDocumentFromNode(node)
	var sub = new(IASubmission)
	id, err := strconv.Atoi(strings.TrimPrefix(sel.Children().Nodes[0].FirstChild.Attr[0].Val, "/job_detail/"))
	if err != nil {
		zap.S().Warn("Invalid ID from ", sel.Children().Nodes[0].FirstChild.Attr[0].Val)
		return nil, err
	}
	sub.ID = id
	sub.Handled = true

	profileAnchor := goquery.NewDocumentFromNode(sel.Children().Nodes[1]).Find("a").First()
	profileLink, ok := profileAnchor.Attr("href")
	if ok {
		parts := strings.Split(profileLink, "/")
		sub.Username = parts[len(parts)-1]
	}
	sub.DisplayName = strings.TrimSpace(profileAnchor.Text())

	problemNode := goquery.NewDocumentFromNode(sel.Children().Nodes[2])
	if strings.TrimSpace(problemNode.Text()) == "..." {
		sub.ProblemID = nil
		sub.ProblemName = nil
	} else {
		problemAnchor := problemNode.Find("a")
		problemLink, ok := problemAnchor.Attr("href")
		if ok {
			parts := strings.Split(problemLink, "/")
			sub.ProblemID = &parts[len(parts)-1]
		}
		name := strings.TrimSpace(problemAnchor.Text())
		sub.ProblemName = &name
	}

	sizeText := strings.TrimSpace(strings.ReplaceAll(goquery.NewDocumentFromNode(sel.Children().Nodes[4]).Text(), "kb", ""))
	if sizeText == "..." {
		sub.SizeKB = nil
	} else {
		size, err := strconv.ParseFloat(sizeText, 64)
		if err != nil {
			zap.S().Warnf("Invalid size string %q (id: %d)", size, sub.ID)
		} else {
			sub.SizeKB = &size
		}
	}

	date := strings.TrimSpace(sel.Children().Nodes[5].FirstChild.Data)
	for k, v := range replacements {
		date = strings.ReplaceAll(date, k, v)
	}
	t, err := time.ParseInLocation(iaFormat, date, location)
	if err != nil {
		zap.S().Info("Invalid time from infoarena", date)
		return nil, errors.New("invalid time")
	}
	sub.Date = t

	statusText := strings.TrimSpace(goquery.NewDocumentFromNode(sel.Children().Nodes[6]).Text())
	if strings.Contains(statusText, "ignorata") {
		sub.Ignored = true
	} else if strings.Contains(statusText, "asteptare") {
		// Waiting
		sub.Handled = false
	} else if strings.Contains(statusText, "evalueaza") {
		// Working
		sub.Handled = false
	} else {
		// Done

		if strings.Contains(statusText, "partiale") {
			sub.Score = nil
		} else {
			parts := strings.SplitN(statusText, ": ", 2)
			var score int
			if len(parts) == 2 {
				if _, err := fmt.Sscanf(parts[1], "%d", &score); err != nil {
					zap.S().Warn("Scanf error: ", err)
				}
				sub.Score = &score
			} else {
				zap.S().Info(sub.ID, statusText)
			}
		}

		if strings.Contains(statusText, "configurarea") || strings.Contains(statusText, "sistem") { // system error or problem config error
			sub.InternalError = true
		} else if strings.Contains(statusText, "compilare") {
			sub.CompileError = true
		}
	}

	return sub, nil
}

const entriesCount = 250

func ParseMonitorPage(ctx context.Context, offset int, jobID *int) ([]*IASubmission, error) {
	var jobid int
	if jobID != nil {
		jobid = *jobID
	}
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://www.infoarena.ro/monitor?display_entries=%d&only_table=true&first_entry=%d&job_id=%d", entriesCount, offset, jobid), nil)
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
	var subs = make([]*IASubmission, 0, entriesCount+10)
	for _, node := range doc.Find("tbody").Children().Nodes {
		sub, err := parseSubmission(node)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

func init() {
	initLogger(true)
	loc, err := time.LoadLocation("Europe/Bucharest")
	if err != nil {
		panic(err)
	}
	location = loc
}
