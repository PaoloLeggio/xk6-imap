package imap

import (
	"io/ioutil"
	"mime/quotedprintable"
	"net/textproto"
	"strconv"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"

	ec "github.com/eugercek/xk6-imap/client"
	"go.k6.io/k6/js/modules"
)

func init() {
	modules.Register("k6/x/imap", new(Imap))
}

type Imap struct{}

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

// Simple function for one time read
// Use EmailClient for more complex needs
func (*Imap) Read(email, password, URL string, port int, headerObj map[string]interface{}) (string, string) {
	c, err := client.DialTLS(URL+":"+strconv.Itoa(port), nil)

	if err != nil {
		return "", err.Error()
	}

	defer c.Logout()

	if err := c.Login(email, password); err != nil {
		return "", err.Error()
	}

	_, err = c.Select("INBOX", true)

	if err != nil {
		return "", err.Error()
	}

	// Converti l'oggetto JavaScript in textproto.MIMEHeader
	header := convertJSObjectToMIMEHeader(headerObj)

	criteria := &imap.SearchCriteria{
		Header: header,
	}

	ids, err := c.Search(criteria)

	if err != nil {
		return "", err.Error()
	}

	if len(ids) == 0 {
		return "", "No messages found"
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(ids...)

	items := []imap.FetchItem{imap.FetchItem("BODY[TEXT]")}

	messages := make(chan *imap.Message, 1)

	err = c.Fetch(seqSet, items, messages)

	if err != nil {
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

	return string(bs), "" // TODO Maybe return "OK"
}

// Create new email client
func (*Imap) EmailClient(email, password, url string, port int) *ec.EmailClient {
	return &ec.EmailClient{
		Email:    email,
		Password: password,
		Url:      url,
		Port:     port,
	}
}
