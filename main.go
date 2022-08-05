package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/schollz/progressbar/v3"

	"github.com/NoStalk/serviceUtilities"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

/**
 * Type definations for unmarshaling RecentACSubmissions Response json
 */
type RecentACSubmissionsResponse struct {
	Data SubmissionData `json:"data"`
}
type SubmissionData struct {
	RecentACSubmissionList []RecentACSubmissionList `json:"recentAcSubmissionList"`
}
type RecentACSubmissionList struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	TitleSlug string `json:"titleSlug"`
	Timestamp string `json:"timestamp"`
}
type RecentQuestions struct {
	ProblemTitle string `json:"problemTitle"`
	CodeLink     string `json:"codeLink"`
}

/**
 * Type definations for unmarshaling userContestHistory Response json
 */
type UserContestHistoryResponse struct {
	Data ConsestData `json:"data"`
}
type ConsestData struct {12.779s
	AttendedContestsCount int64       `json:"attendedContestsCount"`
	Rating                float64     `json:"rating"`
	GlobalRanking         int64       `json:"globalRanking"`
	TotalParticipants     int64       `json:"totalParticipants"`
	TopPercentage         float64     `json:"topPercentage"`
	Badge                 interface{} `json:"badge"`
}
type UserContestRankingHistory struct {
	Attended            bool           `json:"attended"`
	TrendDirection      TrendDirection `json:"trendDirection"`
	ProblemsSolved      int64          `json:"problemsSolved"`
	TotalProblems       int64          `json:"totalProblems"`
	FinishTimeInSeconds int64          `json:"finishTimeInSeconds"`
	Rating              float64        `json:"rating"`
	Ranking             int64          `json:"ranking"`
	Contest             Contest        `json:"contest"`
}
type Contest struct {
	Title     string `json:"title"`
	StartTime int64  `json:"startTime"`
}
type TrendDirection string

const (
	Down TrendDirection = "DOWN"
	None TrendDirection = "NONE"
	Up   TrendDirection = "UP"
)

//TODO implement cookie caching
func logIntoLeetCode(username, password string) chromedp.Tasks {

	userNameInputFeildSelector := `input[name='login']`
	passwordNameInputFeildSelector := `input[name='password']`
	submitButtonSelector := `button#signin_btn`

	return chromedp.Tasks{
		chromedp.Navigate(`https://leetcode.com/accounts/login/`),

		chromedp.WaitReady(userNameInputFeildSelector),
		chromedp.SendKeys(userNameInputFeildSelector, username),

		chromedp.WaitReady(passwordNameInputFeildSelector),
		chromedp.SendKeys(passwordNameInputFeildSelector, password),

		chromedp.WaitReady(submitButtonSelector),
		chromedp.Evaluate(`document.querySelector('button#signin_btn').click()`, nil),
		//Wait for session to be loaded properly
		chromedp.Sleep(7 * time.Second),
	}
}

func evaluateUnixTimeStamp(timeString string) (string, error) {

	if strings.Contains(timeString, "few seconds ago") {
		return time.Now().Format(time.RFC3339), nil
	}

	var seconds int64
	words := strings.Split(timeString, " ")
	var second int64
	var err error

	for i := 0; i < len(words)-1; i += 2 {
		duration, unit := words[i], words[i+1]

		switch unit {
		case "year":
			fallthrough
		case "years":
			second, err = strconv.ParseInt(duration, 10, 64)
			seconds += second * 31536000
		case "month":
			fallthrough
		case "months":
			second, err = strconv.ParseInt(duration, 10, 64)
			seconds += second * 2592000
		case "week":
			fallthrough
		case "weeks":
			second, err = strconv.ParseInt(duration, 10, 64)
			seconds += second * 604800
		case "day":
			fallthrough
		case "days":
			second, err = strconv.ParseInt(duration, 10, 64)
			seconds += second * 86400
		case "houbyter":
			fallthrough
		case "hours":
			second, err = strconv.ParseInt(duration, 10, 64)
			seconds += second * 3600
		case "minute":
			fallthrough
		case "minutes":
			second, err = strconv.ParseInt(duration, 10, 64)
			seconds += second * 60
		case "second":
			fallthrough
		case "seconds":
			seconds += second
		}
	}
	return time.Now().Add(time.Duration(-seconds) * time.Second).Format(time.RFC3339), err
}

func listenForNetworkEvent(ctx context.Context) {

	var recentACSubmissionRequestId network.RequestID
	var recentACSubmissions RecentACSubmissionsResponse
	c := chromedp.FromContext(ctx)
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch ev := ev.(type) {

		case *network.EventRequestWillBeSent:
			req := ev.Request
			if strings.Contains(req.PostData, "recentAcSubmissions") {
				recentACSubmissionRequestId = ev.RequestID
				fmt.Println("set recentACSubmission", recentACSubmissionRequestId)
			}

		case *network.EventResponseReceived:
			switch ev.RequestID {
			case recentACSubmissionRequestId:
				//TODO handle error
				fmt.Println("received recentACSubmission", ev.RequestID)
				byteArr, err := network.GetResponseBody(recentACSubmissionRequestId).Do(cdp.WithExecutor(ctx, c.Target))
				if err != nil {
					log.Println(err)
				}
				fmt.Println(byteArr)
				json.Unmarshal(byteArr, &recentACSubmissions)
				fmt.Println("recentACSubmissions", recentACSubmissions)
			}
		}
	})
}

func getSubmissionDetails(ctx context.Context, submissionLink string) (problemLink string, language string, date string, err error) {
	chromedp.Run(ctx,
		chromedp.Navigate(submissionLink),
		chromedp.Evaluate(`document.querySelector('a.inline-wrap').href`, &problemLink),
		chromedp.Text(`span#result_language`, &language),
	)
	return
}

func fetchDetails(ctx context.Context, username string) (submissions []serviceUtilities.SubmissionData, contests []serviceUtilities.ContestData, err error) {

	var recentACSubmissionRequestId network.RequestID
	var userContestHistoryRequestID network.RequestID
	numberOfRequestsToWaitFor := 2

	done := make(chan bool)
	chromedp.ListenTarget(ctx, func(v interface{}) {
		switch ev := v.(type) {

		case *network.EventRequestWillBeSent:

			if strings.Contains(ev.Request.PostData, "recentAcSubmissions") {
				recentACSubmissionRequestId = ev.RequestID
			} else if strings.Contains(ev.Request.PostData, "userContestRankingInfo") {
				userContestHistoryRequestID = ev.RequestID
			}

		case *network.EventLoadingFinished:

			switch ev.RequestID {
			case recentACSubmissionRequestId:
				fallthrough
			case userContestHistoryRequestID:
				numberOfRequestsToWaitFor--
			}

			if numberOfRequestsToWaitFor == 0 {
				done <- true
			}
			// fmt.Printf("%d, ", numberOfRequestsToWaitFor)
		}
	})

	if err := chromedp.Run(ctx,
		chromedp.Navigate(`https://leetcode.com/`+username),
	); err != nil {
		log.Fatal(err)
	}

	//Wait for responses to arrive
	log.Println("⏳ Wating for responses to arrive...")

	<-done
	logWithTimeStamp("✅ Response recieved successfuly")

	var recentACSubmisssions RecentACSubmissionsResponse
	var userContestHistory UserContestHistoryResponse

	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		buffer, err := network.GetResponseBody(recentACSubmissionRequestId).Do(ctx)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(buffer, &recentACSubmisssions); err != nil {
			return err
		}

		buffer, err = network.GetResponseBody(userContestHistoryRequestID).Do(ctx)
		if err != nil {
			return err
		}

		if err = json.Unmarshal(buffer, &userContestHistory); err != nil {
			return err
		}
		return nil
	}))
	if err != nil {
		return
	}

	//TypeCasting to Database Compatible Format
	log.Println("⏳ Typecasting recentACSubmisssions...")
	bar := getBar(len(recentACSubmisssions.Data.RecentACSubmissionList))
	for _, submission := range recentACSubmisssions.Data.RecentACSubmissionList {

		submissions = append(submissions, serviceUtilities.SubmissionData{
			ProblemName:      submission.Title,
			SubmissionDate:   submission.Timestamp,
			SubmissionStatus: "AC",
			CodeUrl:          `https://leetcode.com/submissions/detail/` + submission.ID,
		})
		bar.Add(1)
	}
	logWithTimeStamp("✅ recentACSubmisssions Typecast done")

	log.Println("⏳ Typecasting userContestHistory...")
	bar = getBar(len(userContestHistory.Data.UserContestRankingHistory))
	for _, contest := range userContestHistory.Data.UserContestRankingHistory {

		if !contest.Attended {
			bar.Add(1)
			continue
		}
		contests = append(contests, serviceUtilities.ContestData{
			ContestName: contest.Contest.Title,
			Rank:        contest.Ranking,
			Rating:      float64(contest.Rating),
			Solved:      int32(contest.ProblemsSolved),
			ContestID:   contest.Contest.Title,
		})

		bar.Add(1)
	}
	logWithTimeStamp("✅ userContestHistory Typecast done")

	fmt.Println(submissions)

	// fetchAdditionalSubmissionDetails(ctx, &submissions)

	return
}

func fetchAdditionalSubmissionDetails(ctx context.Context, submissions *[]serviceUtilities.SubmissionData) {
	log.Println("⏳ Fetching additional submission data...")
	bar := getBar(len(*submissions))
	for i := range *submissions {
		err := chromedp.Run(ctx,
			chromedp.Navigate((*submissions)[i].CodeUrl),
			chromedp.WaitVisible(`span#result_language`, chromedp.ByQuery),
			chromedp.Text(`span#result_language`, &(*submissions)[i].SubmissionLanguage),
			// chromedp.WaitVisible(`a[href]`, chromedp.ByQuery),
			chromedp.Evaluate(`document.querySelector("a.inline-wrap").href`, &(*submissions)[i].ProblemUrl),
		)
		if err != nil {
			log.Println(err.Error())
		}
		bar.Add(1)
	}
	logWithTimeStamp("✅ Additional submission data fetched")
}

var startTime time.Time

func logWithTimeStamp(msg string) {
	log.Printf("%s, took %.3fs\n", msg, time.Since(startTime).Seconds())
}

func getBar(items int) *progressbar.ProgressBar {
	return progressbar.NewOptions(items,
		progressbar.OptionOnCompletion(func() { fmt.Println() }),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSpinnerType(10),
		progressbar.OptionThrottle(time.Second),
	)
}

func main() {

	//Starting time and loading env variables
	startTime = time.Now()
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}
	logWithTimeStamp("✅ Loaded enviroment variables")

	//Create a headless chrome context
	log.Println("⏳ creating chormedp context...")
	ctx, cancel := chromedp.NewContext(
		context.Background(),
		chromedp.WithLogf(log.Printf),
	)
	defer cancel()

	//Add timeout to the context
	ctx, cancel = context.WithTimeout(ctx, time.Minute)
	defer cancel()
	logWithTimeStamp("✅ Created chromecp context")

	//Login to leetcode
	log.Println("⏳ logging into leetcode...")
	chromedp.Run(ctx, logIntoLeetCode(os.Getenv(`LEETCODE_USERNAME`), os.Getenv(`LEETCODE_PASSWORD`)))
	logWithTimeStamp("✅ Log in to leetcode successfull")

	//Starting fetch

	log.Println("⏳ starting data fetch...")
	// submissions, contests, err := fetchDetails(ctx, `SanpuiRonak`)
	var test []serviceUtilities.SubmissionData
	test = append(test, serviceUtilities.SubmissionData{
		CodeUrl: "https://leetcode.com/submissions/detail/764090438/",
	})
	fetchAdditionalSubmissionDetails(ctx, &test)
	// if err != nil {
	// 	log.Fatalf(err.Error())
	// }
	logWithTimeStamp("✅ Fetch successful")

	// for _, submission := range submissions {
	// 	fmt.Println(submission)
	// }
	// for _, contest := range contests {
	// 	fmt.Println(contest)
	// }

}
