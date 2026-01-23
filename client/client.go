package client

import (
	"fmt"
	"io/ioutil"
	"mime/quotedprintable"
	"net/textproto"
	"strconv"
	"time"

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
	fmt.Println("Read called with headerObj:", headerObj)
	
	// Verifica che il client sia connesso
	if e.client == nil {
		return "", "Client not connected. Call login() first."
	}
	
	_, err := e.client.Select("INBOX", true)
	if err != nil {
		fmt.Printf("Error selecting INBOX: %v\n", err)
		return "", err.Error()
	}

	// Converti l'oggetto JavaScript in textproto.MIMEHeader
	header := convertJSObjectToMIMEHeader(headerObj)
	fmt.Printf("Converted header: %+v\n", header)

	criteria := &imap.SearchCriteria{
		Header: header,
	}

	ids, err := e.client.Search(criteria)
	if err != nil {
		fmt.Printf("Error searching: %v\n", err)
		return "", err.Error()
	}

	fmt.Printf("Found %d message IDs\n", len(ids))

	if len(ids) == 0 {
		return "", "No messages found"
	}

	// Prendi solo il primo messaggio (il più recente, ultimo ID)
	latestID := ids[len(ids)-1]
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(latestID)

	items := []imap.FetchItem{imap.FetchItem("BODY[TEXT]")}
	messages := make(chan *imap.Message, 1)

	fmt.Printf("Fetching message ID %d...\n", latestID)
	err = e.client.Fetch(seqSet, items, messages)
	if err != nil {
		fmt.Printf("Error fetching: %v\n", err)
		return "", err.Error()
	}

	fmt.Println("Fetch completed, reading from channel...")
	msg := <-messages
	fmt.Println("Message received from channel")
	
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

	fmt.Println("Read successful, message length:", len(bs))
	return string(bs), ""

}

func (e *EmailClient) WaitNewEmail(headerObj map[string]interface{}, timeoutMs int64) (string, string) {
	fmt.Println("WaitNewEmail started")
	startTime := time.Now()
	timeoutDuration := time.Duration(timeoutMs) * time.Millisecond
	
	// Converti l'oggetto JavaScript in textproto.MIMEHeader
	header := convertJSObjectToMIMEHeader(headerObj)
	
	// Polling ogni 2 secondi
	pollInterval := 2 * time.Second
	
	for {
		// Controlla se il timeout è scaduto
		if time.Since(startTime) >= timeoutDuration {
			return "", fmt.Sprintf("Timeout: no new email found within %d ms", timeoutMs)
		}
		
		// Seleziona la mailbox
		_, err := e.client.Select("INBOX", true)
		if err != nil {
			fmt.Println(err)
			return "", err.Error()
		}
		
		// Crea i criteri di ricerca solo con i filtri Header (senza Since)
		criteria := &imap.SearchCriteria{
			Header: header,
		}
		
		ids, err := e.client.Search(criteria)
		if err != nil {
			fmt.Println(err)
			return "", err.Error()
		}
		
		// Se troviamo email, verifica manualmente la data
		if len(ids) > 0 {
			// Ordina gli ID in ordine decrescente per controllare le più recenti prima
			// Gli ID sono già ordinati crescente, quindi prendiamo dall'ultimo
			for i := len(ids) - 1; i >= 0; i-- {
				msgID := ids[i]
				
				seqSet := new(imap.SeqSet)
				seqSet.AddNum(msgID)
				
				// Recupera ENVELOPE per ottenere la data del messaggio
				items := []imap.FetchItem{imap.FetchItem("ENVELOPE"), imap.FetchItem("BODY[TEXT]")}
				messages := make(chan *imap.Message, 1)
				
				err = e.client.Fetch(seqSet, items, messages)
				if err != nil {
					fmt.Println(err)
					continue
				}
				
				msg := <-messages
				if msg == nil {
					continue
				}
				
				// Verifica che la data del messaggio sia successiva a startTime
				if msg.Envelope != nil && msg.Envelope.Date.After(startTime) {
					// Questa è una nuova email, recupera il body
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
			}
		}
		
		// Aspetta prima del prossimo polling
		time.Sleep(pollInterval)
	}
}

func (e *EmailClient) Logout() {
	if e.client != nil {
		e.client.Logout()
	}
}
