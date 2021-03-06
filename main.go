package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/costexplorer"
	"github.com/jinzhu/now"
)

type SlackRequestBody struct {
	Text string `json:"text"`
}

type BillingRange struct {
	thisMonth, lastMonth time.Time
}

func (br *BillingRange) DateRangeString() string {
	return br.lastMonthString() + " - " + br.thisMonthString()
}

func (br *BillingRange) thisMonthString() string {
	return br.thisMonth.Format("2006-01-02")
}

func (br *BillingRange) lastMonthString() string {
	return br.lastMonth.Format("2006-01-02")
}

func main() {
	lambda.Start(SendReport)
}

func SendReport() {

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-west-2")},
	)

	// Create costexplorer client
	client := costexplorer.New(sess)

	gdByTag := &costexplorer.GroupDefinition{
		Type: aws.String("TAG"),
		Key:  aws.String("Billing")}

	BillingDates := BillingRange{
		thisMonth: now.BeginningOfMonth(),
		lastMonth: now.BeginningOfMonth().AddDate(0, -1, 0)}
	// Specify the details of the instance that you want to create.
	resp, err := client.GetCostAndUsage(&costexplorer.GetCostAndUsageInput{
		Granularity: aws.String("MONTHLY"),
		Metrics:     []*string{aws.String("BLENDED_COST")},
		GroupBy:     []*costexplorer.GroupDefinition{gdByTag},
		TimePeriod: &costexplorer.DateInterval{
			End:   aws.String(BillingDates.thisMonthString()),
			Start: aws.String(BillingDates.lastMonthString())}})

	SlackMessage := BuildSlackMessage(resp)

	if err != nil {
		fmt.Println("Some error happened ", err)
		return
	}

	// Send Slack notification usign environmental variable
	webhookUrl := os.Getenv("SLACK_WEBHOOK_URL")
	err = SendSlackNotification(webhookUrl, SlackMessage)
	if err != nil {
		log.Fatal(err)
	}
}

// SendSlackNotification will post to an 'Incoming Webook' url setup in Slack Apps. It accepts
// some text and the slack channel is saved within Slack.
func SendSlackNotification(webhookUrl string, msg string) error {

	slackBody, _ := json.Marshal(SlackRequestBody{Text: msg})
	req, err := http.NewRequest(http.MethodPost, webhookUrl, bytes.NewBuffer(slackBody))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	if buf.String() != "ok" {
		return errors.New("Non-ok response returned from Slack")
	}
	return nil
}

// function for checkin if the projectin ignorelist
func isIgnored(str *string) bool {
	// todo: read arr list from env vars
	ignoreList := strings.Split(os.Getenv("BILLING_IGNORE_LIST"), ",")
	for _, project := range ignoreList {
		if project == (*str)[8:] {
			return true
		}
	}
	return false

}

func BuildSlackMessage(AwsBillingResponse *costexplorer.GetCostAndUsageOutput) string {

	BillingDates := BillingRange{
		thisMonth: now.BeginningOfMonth(),
		lastMonth: now.BeginningOfMonth().AddDate(0, -1, 0)}

	SlackMessage := BillingDates.DateRangeString() + "\nProjects hardware expences (AWS): \n"
	fmt.Println(AwsBillingResponse)
	for i, d := range AwsBillingResponse.ResultsByTime[0].Groups {
		if i > 0 {
			// go throught keys and select them if they aren't belong to ignore list / nil
			if d.Keys != nil && !isIgnored(d.Keys[0]) {
				// round up to cents the instances cost
				projectCostsFloat, err := strconv.ParseFloat(*d.Metrics["BlendedCost"].Amount, 32)
				projectCosts := fmt.Sprintf("%.2f", projectCostsFloat)
				projectName := *d.Keys[0]
				SlackMessage += "• " + projectName[8:] + ": $" + projectCosts + "\n"

				if err != nil {
					fmt.Println("Some error happened ", err)
					return "err"
				}
			}
		}
	}
	return SlackMessage

}
