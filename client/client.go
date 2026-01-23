package client

import (
	"fmt"
	"io/ioutil"
	"mime/quotedprintable"
	"net/textproto"
	"strconv"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type EmailClient struct {
	Email    string
	Password string
	Url      string
	Port     int
	client   *client.Client
}

// convertJSObjectToMIMEHeader converte un oggetto JavaScript in textproto.MIMEHeader
// Gestisce la conversione da map[string]interface{} (come viene passato da k6) a textproto.MIMEHeader
func convertJSObjectToMIMEHeader(obj map[string]interface{}) textproto.MIMEHeader {
	header := make(textproto.MIMEHeader)
	
	for key, value := range obj {
		switch v := value.(type) {
		case []interface{}:
			// Array di valori JavaScript
			values := make([]string, 0, len(v))
			for _, item := range v {
				if str, ok := item.(string); ok {
					values = append(values, str)
				}
			}
			if len(values) > 0 {
				header[key] = values
			}
		case string:
			// Singola stringa
			header[key] = []string{v}
		case []string:
			// Già un array di stringhe (caso raro ma possibile)
			header[key] = v
		}
	}
	
	return header
}

func (e *EmailClient) Login() string {
	c, err := client.DialTLS(e.Url+":"+strconv.Itoa(e.Port), nil)

	if err != nil {
		return err.Error()
	}

	e.client = c

	err = e.client.Login(e.Email, e.Password)

	if err != nil {
		return err.Error()
	}

	return ""

}

func (e *EmailClient) Read(headerObj map[string]interface{}) (string, string) {
	_, err := e.client.Select("INBOX", true)

	if err != nil {
		fmt.Println(err)
		return "", err.Error()
	}

	// Converti l'oggetto JavaScript in textproto.MIMEHeader
	header := convertJSObjectToMIMEHeader(headerObj)

	criteria := &imap.SearchCriteria{
		Header: header,
	}

	ids, err := e.client.Search(criteria)

	if err != nil {
		fmt.Println(err)
		return "", err.Error()
	}

	if len(ids) == 0 {
		return "", "No messages found"
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(ids...)

	items := []imap.FetchItem{imap.FetchItem("BODY[TEXT]")}

	messages := make(chan *imap.Message, 1)

	err = e.client.Fetch(seqSet, items, messages)

	if err != nil {
		fmt.Println(err)
		return "", err.Error()
	}

	msg := <-messages

	if msg == nil {
		return "", "No message"
	}

	section, _ := imap.ParseBodySectionName("BODY[TEXT]")
	r := msg.GetBody(section)

	if r == nil {
		return "", "Could not get message body"
	}

	qr := quotedprintable.NewReader(r)
	bs, err := ioutil.ReadAll(qr)

	if err != nil {
		return "", err.Error()
	}

	return string(bs), ""

}

func (e *EmailClient) Logout() {
	if e.client != nil {
		e.client.Logout()
	}
}
