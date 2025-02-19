package email

import (
	"github.com/apex/log"
)

func SendEmail(recipients []string) error {
	log.Infof("!!!Sending email to %d recipients!!!", len(recipients))
	return nil
}
