package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"log"
	"strings"
	"time"

	"github.com/NoStalk/protoDefinitions"
	"google.golang.org/grpc"

	"github.com/joho/godotenv"
	"github.com/schollz/progressbar/v3"

	"github.com/NoStalk/serviceUtilities"
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
type ConsestData struct {
	UserContestRanking        UserContestRanking          `json:"userContestRanking"`
	UserContestRankingHistory []UserContestRankingHistory `json:"userContestRankingHistory"`
}
type UserContestRanking struct {
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

type server struct {
	platformDatapb.UnimplementedFetchPlatformDataServer
}

func WaitTillSessionCookieIsSet() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for {
			cookies, err := network.GetAllCookies().Do(ctx)
			if err != nil {
				return err
			}
			for _, cookie := range cookies {
				if cookie.Name == "LEETCODE_SESSION" {
					return nil
				}
			}
			time.Sleep(2 * time.Second)
		}
	})
}

//TODO implement cookie caching
func logIntoLeetCode(ctx context.Context, username, password string) error {

	log.Println("⏳ logging into leetcode...")

	userNameInputFeildSelector := `input[name='login']`
	passwordNameInputFeildSelector := `input[name='password']`
	submitButtonSelector := `button#signin_btn`

	err := chromedp.Run(
		ctx,
		chromedp.Navigate(`https://leetcode.com/accounts/login/`),

		chromedp.WaitReady(userNameInputFeildSelector),
		chromedp.SendKeys(userNameInputFeildSelector, username),

		chromedp.WaitReady(passwordNameInputFeildSelector),
		chromedp.SendKeys(passwordNameInputFeildSelector, password),

		chromedp.WaitReady(submitButtonSelector),
		chromedp.Evaluate(`document.querySelector('button#signin_btn').click()`, nil),

		WaitTillSessionCookieIsSet(),
	)
	if err != nil {
		return err
	}
	logWithTimeStamp("✅ Log in to leetcode successfull")
	return nil
}

func fetchDetails(ctx context.Context, username string) (submissions []serviceUtilities.SubmissionData, contests []serviceUtilities.ContestData, err error) {
	log.Println("⏳ starting data fetch...")

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
		}
	})

	err = chromedp.Run(ctx,
		chromedp.Navigate(`https://leetcode.com/`+username),
	)
	if err != nil {
		return
	}

	//Wait for responses to arrive
	log.Println("⏳ Wating for responses to arrive...")

	<-done
	logWithTimeStamp("✅ Response recieved successfuly")

	var recentACSubmisssions RecentACSubmissionsResponse
	var userContestHistory UserContestHistoryResponse

	err = chromedp.Run(
		ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			buffer, err := network.GetResponseBody(recentACSubmissionRequestId).Do(ctx)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(buffer, &recentACSubmisssions); err != nil {
				return err
			}

			if buffer, err = network.GetResponseBody(userContestHistoryRequestID).Do(ctx); err != nil {
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

	// err = fetchAdditionalSubmissionDetails(ctx, &submissions)
	if err != nil {
		return
	}

	logWithTimeStamp("✅ Fetch successful")
	return
}

func fetchAdditionalSubmissionDetails(ctx context.Context, submissions *[]serviceUtilities.SubmissionData) error {
	log.Println("⏳ Fetching additional submission data...")
	bar := getBar(len(*submissions))

	for i := range *submissions {

		fmt.Printf("%+v\n", (*submissions)[i].CodeUrl)
		err := chromedp.Run(ctx,
			chromedp.Navigate((*submissions)[i].CodeUrl),
			// chromedp.Evaluate(`window.location.href`, debugUrl),
			// chromedp.WaitVisible(`span#result_language`, chromedp.ByQuery),
			// chromedp.Text(`span#result_language`, &(*submissions)[i].SubmissionLanguage),
			// chromedp.Evaluate(`document.querySelector("a.inline-wrap").href`, &(*submissions)[i].ProblemUrl),
		)
		if err != nil {
			return err
		}
		bar.Add(1)
	}

	logWithTimeStamp("✅ Additional submission data fetched")
	return nil
}

func (*server) GetUserSubmissions(ctx context.Context, req *platformDatapb.Request) (*platformDatapb.SubmissionResponse, error) {
	log.Println("GetUserSubmission function invoked")
	_, _, err := fetchDetails(chromedpContext, req.GetUserHandle())
	if err != nil {
		log.Println(err.Error())
	}
	submissionGRPCArray := []*platformDatapb.Submission{{
		Language: "kotlin",
	}}
	submissionGRPCResponse := &platformDatapb.SubmissionResponse{
		Submissions: submissionGRPCArray,
	}

	return submissionGRPCResponse, nil
}

var chromedpContext context.Context
var chromedpCancel context.CancelFunc

func main() {
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)
	//Starting time and loading env variables
	startTime = time.Now()
	if err := godotenv.Load(); err != nil {
		log.Fatalln("Error loading .env file")
	}
	logWithTimeStamp("✅ Loaded enviroment variables")

	//Create a headless chrome context
	log.Println("⏳ creating chormedp context...")
	chromedpContext, chromedpCancel = chromedp.NewContext(
		context.Background(),
		chromedp.WithLogf(log.Printf),
	)
	defer chromedpCancel()

	//Add timeout to the context
	chromedpContext, chromedpCancel = context.WithTimeout(chromedpContext, time.Minute)
	defer chromedpCancel()
	logWithTimeStamp("✅ Created chromedp context")

	//Login to leetcode
	// if err := logIntoLeetCode(ctx, os.Getenv(`LEETCODE_USERNAME`), os.Getenv(`LEETCODE_PASSWORD`)); err != nil {
	// 	log.Fatalln(err.Error())
	// }

	//Start the server
	lis, err := net.Listen("tcp", "0.0.0.0:5003")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer()
	platformDatapb.RegisterFetchPlatformDataServer(s, &server{})
	logWithTimeStamp("✅ Server started on port 5003")
	if err = s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}

	//Starting fetch
	// submissions, contests, err := fetchDetails(ctx, `SanpuiRonak`)
	// if err != nil {
	// 	log.Fatalln(err.Error())
	// }

	// log.Println("⏳ Writing to database...")
	// dbInstance, err := serviceUtilities.OpenDatabaseConnection(os.Getenv(`MONGODB_URI`))
	// if err != nil {
	// 	log.Fatalln(err.Error())
	// }
	// if err := serviceUtilities.AppendContestData(dbInstance, "r@g.com", "Leetcode", contests); err != nil {
	// 	log.Fatalln(err.Error())
	// }
	// if err := serviceUtilities.AppendSubmissionData(dbInstance, "r@g.com", "Leetcode", submissions); err != nil {
	// 	log.Fatalln(err.Error())
	// }
	// serviceUtilities.CloseDatabaseConnection(dbInstance)
	// logWithTimeStamp("✅ Writing to database successful")

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
