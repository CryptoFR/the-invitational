package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/nlopes/slack"
	"net/http"
	"os"
)

type Invitation struct {
	Email string
}

type Submission struct {
	Data Payload
}

type Payload map[string]interface{}

type InvitePayload struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

type AcceptPayload struct {
	CallbackID string                   `json:"callback_id"`
	Token      string                   `json:"token"`
	Actions    []slack.AttachmentAction `json:"actions"`
}

type Config struct {
	SlackWebhookURL        string
	SlackInviteURL         string
	SlackVerificationToken string
	SlackAccessToken       string
}

var submissions = make(chan Submission, 1000)
var invitations = make(chan Invitation, 1000)
var config Config

func main() {
	port := getEnv("PORT", "8080")
	slackOrgName := mustGetEnv("SLACK_ORG_NAME")
	config.SlackWebhookURL = mustGetEnv("SLACK_WEBHOOK_URL")
	config.SlackInviteURL = fmt.Sprintf("https://%s.slack.com/api/users.admin.invite", slackOrgName)
	config.SlackVerificationToken = mustGetEnv("SLACK_VERIFICATION_TOKEN")
	config.SlackAccessToken = mustGetEnv("SLACK_ACCESS_TOKEN")

	go message(config.SlackWebhookURL, submissions)
	go invite(config.SlackInviteURL, config.SlackAccessToken, invitations)

	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	e.GET("/", index)
	e.POST("/review", submit)
	e.POST("/accept", accept)
	e.Logger.Fatal(e.Start(":" + port))
}

func index(c echo.Context) error {
	return c.String(200, "OK")
}

// Receive Submission from External Sources
func submit(c echo.Context) error {
	var payload Payload
	if err := c.Bind(&payload); err != nil {
		return err
	}
	fmt.Printf("%v\n", payload)
	submissions <- Submission{Data: payload}
	return c.JSON(http.StatusOK, len(submissions))
}

func accept(c echo.Context) error {
	raw := c.FormValue("payload")
	var payload AcceptPayload
	json.Unmarshal([]byte(raw), &payload)
	if payload.Token != config.SlackVerificationToken {
		return c.NoContent(http.StatusBadRequest)
	}
	action := payload.Actions[0]
	if action.Name == "action" && action.Value == "accept" {
		invitations <- Invitation{Email: payload.CallbackID}
	}
	msg := slack.Msg{Text: "Done"}

	return c.JSON(http.StatusOK, msg)
}

// Send Interactive Message to Slack
func message(url string, jobs chan Submission) {
	for job := range jobs {
		body := new(bytes.Buffer)
		data, err := json.Marshal(job.Data)
		if err != nil {
			fmt.Println(err)
			continue
		}
		email, ok := job.Data["email"]
		if !ok {
			continue
		}

		attachments := []slack.Attachment{attachment(email.(string), string(data))}
		msg := slack.Msg{
			Text:        "Halo!",
			Attachments: attachments,
		}
		err = json.NewEncoder(body).Encode(msg)
		if err != nil {
			fmt.Println(err)
			jobs <- job
		} else {
			_, err = http.Post(url, "application/json", body)
			if err != nil {
				fmt.Println(err)
				jobs <- job
			}
		}

	}
}

func invite(url string, token string, jobs chan Invitation) {
	for job := range jobs {
		body := new(bytes.Buffer)
		payload := InvitePayload{Email: job.Email, Token: token}
		err := json.NewEncoder(body).Encode(payload)
		if err != nil {
			fmt.Println(err)
			jobs <- job
		} else {
			_, err = http.Post(url, "application/x-www-form-urlencoded", body)
			if err != nil {
				fmt.Println(err)
				jobs <- job
			}
		}
	}
}

func attachment(email, data string) slack.Attachment {
	var actions []slack.AttachmentAction
	actions = append(actions, slack.AttachmentAction{
		Name:  "action",
		Text:  "Accept",
		Type:  "button",
		Value: "accept",
		Style: "primary",
	})
	actions = append(actions, slack.AttachmentAction{
		Name:  "action",
		Text:  "Reject",
		Type:  "button",
		Value: "reject",
		Style: "danger",
	})
	value := slack.Attachment{
		Text:       data,
		CallbackID: email,
		Actions:    actions,
	}
	return value
}

func mustGetEnv(key string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	panic(fmt.Sprintf("Environment Variable %s missing", key))
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
