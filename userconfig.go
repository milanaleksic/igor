package igor

import (
	"log"
	"regexp"
	"time"

	"bytes"
	"fmt"
	"text/template"

	"github.com/milanaleksic/flowdock"
)

//UserConfig is a core structure that can communicate with Flowdock and is based on the contents stored in DB
type UserConfig struct {
	Identity                string
	activeFrom, activeUntil time.Time
	lastCommunicationWith   map[string]time.Time
	template                *template.Template
	client                  *flowdock.Client
	nameRegex               *regexp.Regexp
}

// New creates a new UserConfig structure
func New(identity, messageFormat, flowdockUsername, flowdockToken string, activeFrom, activeUntil time.Time, lastCommunication map[string]time.Time) *UserConfig {
	templ, err := template.New("template").Parse(messageFormat)
	if err != nil {
		panic(err)
	}
	return &UserConfig{
		Identity:              identity,
		activeFrom:            activeFrom,
		activeUntil:           activeUntil,
		template:              templ,
		nameRegex:             regexp.MustCompile(fmt.Sprintf("(?i)@%s", flowdockUsername)),
		client:                flowdock.NewClient(flowdockToken),
		lastCommunicationWith: lastCommunication,
	}
}

// IsActive returns true if the configuration table contains "active" configuration with value "true"
func (userConfig *UserConfig) IsActive() bool {
	return time.Now().Before(userConfig.activeUntil) && time.Now().After(userConfig.activeFrom)
}

// GetNonAnsweredMentions returns when was direction mention last received, per user that executed the mention
func (userConfig *UserConfig) GetNonAnsweredMentions() (result map[string]*MentionContext) {
	result = make(map[string]*MentionContext)
	if mentions, err := userConfig.client.GetMyMentions(10); err != nil {
		log.Fatalf("Could not fetch flowdock mentions because of: %+v", err)
	} else {
		for _, mention := range mentions {
			userConfig.addMessageToResult(mention.Message, result)
		}
	}
	if privateMessages, err := userConfig.client.GetMyUnreadPrivateMessages(); err != nil {
		log.Fatalf("Could not fetch flowdock private message because of: %+v", err)
	} else {
		for _, privateMessage := range privateMessages {
			userConfig.addMessageToResult(privateMessage.Message, result)
		}
	}
	return
}

func (userConfig *UserConfig) addMessageToResult(message flowdock.MessageEvent, result map[string]*MentionContext) {
	if message.UserID == "0" {
		// HAL or some other app
		return
	}
	if message.Flow != "" && len(userConfig.nameRegex.FindStringIndex(message.Content)) == 0 {
		// ignoring if no explicit mention
		return
	}
	mentionMoment := time.Unix(message.Timestamp/1000, 0)
	user := userConfig.client.DetailsForUser(message.UserID)
	if mentionMoment.Before(userConfig.activeFrom) {
		return
	}
	lastComm, ok := userConfig.lastCommunicationWith[user.Nick]
	if ok && lastComm.After(mentionMoment) {
		return
	}
	if _, ok := result[user.Nick]; !ok {
		result[user.Nick] = &MentionContext{
			Message:  message.Content,
			Moment:   time.Unix(message.Timestamp/1000, 0),
			Flow:     message.Flow,
			ThreadID: message.ThreadID,
			User:     user.Nick,
			UserID:   user.ID,
		}
	}

}

// RespondToFlow allows to send a message to a certain flow/thread using Flowdock client
func (userConfig *UserConfig) RespondToFlow(flow, thread, siteLocation string) error {
	msg, err := userConfig.GetResponseMessage()
	if err != nil {
		return fmt.Errorf("Could not answer to flow %s, thread %s because of %+v", flow, thread, err)
	}
	msgWithSuffix := addSuffix(msg, siteLocation)
	log.Printf("Responding to flow %s, thread %s, msg %s", flow, thread, msgWithSuffix)
	return userConfig.client.RespondToFlow(flow, thread, msgWithSuffix)
}

// RespondToPerson allows to send a private message to a certain user using Flowdock client
func (userConfig *UserConfig) RespondToPerson(userID int64, siteLocation string) error {
	msg, err := userConfig.GetResponseMessage()
	if err != nil {
		return fmt.Errorf("Could not answer to user %d because of %+v", userID, err)
	}
	msgWithSuffix := addSuffix(msg, siteLocation)
	log.Printf("Responding to user %d, msg %s", userID, msgWithSuffix)
	return userConfig.client.RespondToPerson(userID, msgWithSuffix)
}

func addSuffix(mainContents, suffix string) string {
	return mainContents + fmt.Sprintf(" Powered by [Igor](%s)", suffix)
}

// GetResponseMessage will return the active reponse message
func (userConfig *UserConfig) GetResponseMessage() (string, error) {
	buff := new(bytes.Buffer)
	dataForTemplate := struct {
		From  string
		Until string
	}{
		From:  userConfig.activeFrom.Format(time.RFC822),
		Until: userConfig.activeUntil.Format(time.RFC822),
	}

	err := userConfig.template.Execute(buff, dataForTemplate)
	if err != nil {
		return "", err
	}
	return buff.String(), nil
}
