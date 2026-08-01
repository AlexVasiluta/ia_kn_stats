package algocode_scraper

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"vasiluta.ro/ia_kn_stats/algocode_scraper/algocodeclient"
	"vasiluta.ro/ia_kn_stats/scraper"
)

const entriesCount int64 = 50

func ParseSubmissionsPage(ctx context.Context, host string, offset int64) ([]*scraper.Submission, error) {
	cl, err := algocodeclient.NewClientWithResponses(host) //, algocodeclient.WithHTTPClient(&http.Client{Transport: client}))
	if err != nil {
		return nil, err
	}

	resp, err := cl.PublicGetSubmissionsWithResponse(ctx, algocodeclient.PublicGetSubmissionsJSONRequestBody{Limit: new(entriesCount), Offset: new(offset)})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("Expected 200 but got %d", resp.StatusCode())
	}

	results := make([]*scraper.Submission, 0, len(*resp.JSON200.Submissions))

	for _, sub := range *resp.JSON200.Submissions {
		user, ok := resp.JSON200.Users[strconv.FormatInt(sub.UserId, 10)]
		if !ok {
			slog.WarnContext(ctx, "Unable to get user in algocode request")
		}
		problem, ok := resp.JSON200.Problems[strconv.FormatInt(sub.ProblemId, 10)]
		if !ok {
			slog.WarnContext(ctx, "Unable to get problem in algocode request")
		}

		var score int
		switch val := sub.Score.(type) {
		case int:
			score = val
		case int64:
			score = int(val)
		case float64:
			score = int(val)
		case string:
			scoreInt, _ := strconv.Atoi(val)
			score = scoreInt
		default:
			slog.WarnContext(ctx, "Unknown score type")
		}

		var compileErr bool
		if sub.CompileError != nil {
			compileErr = *sub.CompileError
		}

		sub := &scraper.Submission{
			ID:          int(sub.Id),
			Username:    strconv.FormatInt(sub.UserId, 10),
			DisplayName: user.Name,
			ProblemID:   new(strconv.FormatInt(sub.ProblemId, 10)),
			ProblemName: new(problem.Name),
			SizeKB:      new(float64(sub.CodeSize) / 1024.0), // TODO
			Date:        sub.CreatedAt,

			Ignored:       false,
			CompileError:  compileErr,
			InternalError: false,      // TODO
			Score:         new(score), // TODO

			Handled: sub.Status == "finished",
		}

		results = append(results, sub)
	}

	return results, nil
}

var _ scraper.Parser[int] = &AlgolympParser{}

type AlgolympParser struct {
	Host string
}

func (p *AlgolympParser) PageZeroOffset() int {
	return 0
}

func (p *AlgolympParser) FurthestOffset(ctx context.Context, db *scraper.DB) (int, error) {
	return db.CountSubmissions(ctx)
}

func (p *AlgolympParser) NextPageOffset(t int, subs []*scraper.Submission) int {
	return t + len(subs)
}

func (p *AlgolympParser) GetPage(ctx context.Context, offset int) ([]*scraper.Submission, error) {
	return ParseSubmissionsPage(ctx, p.Host, int64(offset))
}
