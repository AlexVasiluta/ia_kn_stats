package csacademyscraper

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.uber.org/zap"
	"vasiluta.ro/ia_kn_stats/scraper"
)

type csaUser struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Username    any    `json:"username"`
	DisplayName bool   `json:"displayName"`
}

type csaJob struct {
	ID                    int             `json:"id"`
	UserID                int             `json:"userId"`
	TimeSubmitted         float64         `json:"timeSubmitted"`
	SourceText            string          `json:"sourceText"`
	SourceName            string          `json:"sourceName"`
	ProgrammingLanguageID int             `json:"programmingLanguageId"`
	CompileStarted        bool            `json:"compileStarted"`
	CompileOK             bool            `json:"compileOK"`
	Duration              float64         `json:"duration"`
	CompilerMessage       string          `json:"compilerMessage"`
	IsDone                bool            `json:"isDone"`
	StatusStream          string          `json:"statusStream"`
	Comment               any             `json:"comment"`
	IsPinned              bool            `json:"isPinned"`
	Score                 float64         `json:"score"`
	Tests                 json.RawMessage `json:"tests"`
	ContestID             int             `json:"contestId"`
	ContestTaskID         int             `json:"contestTaskId"`
	EvalTaskID            int             `json:"evalTaskId"`
	OnlyExamples          bool            `json:"onlyExamples"`
	ExamplesPassed        bool            `json:"examplesPassed"`
	ExpectedResult        any             `json:"expectedResult"`
}

type CSAResponse struct {
	State struct {
		EvalJob    []csaJob  `json:"evaljob"`
		PublicUser []csaUser `json:"publicuser"`
	} `json:"state"`
	JobCount int `json:"jobCount"`
}

var _ scraper.Parser[*time.Time] = &CSAParser{}

type CSAParser struct{}

func (p *CSAParser) PageZeroOffset() *time.Time {
	return nil
}

func (p *CSAParser) FurthestOffset(ctx context.Context, db *scraper.DB) (*time.Time, error) {
	return db.GetFurthestTime(ctx)
}

func (p *CSAParser) NextPageOffset(t *time.Time, subs []*scraper.Submission) *time.Time {
	for _, sub := range subs {
		if t == nil || sub.Date.Before(*t) {
			tt := sub.Date
			t = &tt
		}
	}
	return t
}

func (p *CSAParser) GetPage(ctx context.Context, offset *time.Time) ([]*scraper.Submission, error) {
	q := "?numJobs=100"
	if offset != nil {
		q += "&endTime=" + strconv.FormatInt(offset.Unix(), 10)
	}
	url := url.URL{
		Scheme:   "https",
		Host:     "csacademy.com",
		Path:     "/eval/get_eval_jobs/",
		RawQuery: q,
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("x-requested-with", "XMLHttpRequest")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data CSAResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var users = make(map[int]csaUser)
	for _, user := range data.State.PublicUser {
		users[user.ID] = user
	}

	subs := make([]*scraper.Submission, 0, len(data.State.EvalJob))
	for _, job := range data.State.EvalJob {
		user, ok := users[job.UserID]
		if !ok {
			zap.S().Warn("Could not find user")
			user = csaUser{ID: -1, Username: "", Name: ""}
		}

		pbid := strconv.Itoa(job.EvalTaskID)
		size := len(job.SourceText)
		var sizeKB *float64
		if size <= 0 {
		} else {
			kb := float64(size) / 1024.0
			sizeKB = &kb
		}
		sec, dec := math.Modf(job.TimeSubmitted)
		scc := job.Score
		var score *int
		if !(scc < 0 || math.IsNaN(scc)) {
			s := int(scc * 100)
			score = &s
		}

		subs = append(subs, &scraper.Submission{
			ID:            job.ID,
			Username:      strconv.Itoa(job.UserID),
			DisplayName:   user.Name,
			ProblemID:     &pbid,
			ProblemName:   nil,
			SizeKB:        sizeKB,
			Date:          time.Unix(int64(sec), int64(dec*1e9)),
			Ignored:       false, // TODO: ?
			CompileError:  !job.CompileOK,
			InternalError: false, // TODO: ?
			Handled:       job.IsDone,
			Score:         score,
		})
	}
	return subs, nil
}
