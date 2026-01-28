package client

import (
	"fmt"
	"io/ioutil"
	"mime/quotedprintable"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/grafana/sobek"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/js/promises"
)

type EmailClient struct {
	Vu         modules.VU
	Email      string
	Password   string
	Url        string
	Port       int
	client     *client.Client
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

// messageToMap converte un *imap.Message in un map[string]interface{} compatibile con k6/JavaScript
func messageToMap(msg *imap.Message) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	
	// Subject
	if msg.Envelope != nil {
		result["subject"] = msg.Envelope.Subject
		
		// From
		if len(msg.Envelope.From) > 0 {
			fromAddrs := make([]string, 0, len(msg.Envelope.From))
			for _, addr := range msg.Envelope.From {
				if addr.MailboxName != "" && addr.HostName != "" {
					fromAddrs = append(fromAddrs, fmt.Sprintf("%s@%s", addr.MailboxName, addr.HostName))
				} else if addr.MailboxName != "" {
					fromAddrs = append(fromAddrs, addr.MailboxName)
				}
			}
			if len(fromAddrs) == 1 {
				result["from"] = fromAddrs[0]
			} else {
				result["from"] = fromAddrs
			}
		}
		
		// To
		if len(msg.Envelope.To) > 0 {
			toAddrs := make([]string, 0, len(msg.Envelope.To))
			for _, addr := range msg.Envelope.To {
				if addr.MailboxName != "" && addr.HostName != "" {
					toAddrs = append(toAddrs, fmt.Sprintf("%s@%s", addr.MailboxName, addr.HostName))
				} else if addr.MailboxName != "" {
					toAddrs = append(toAddrs, addr.MailboxName)
				}
			}
			result["to"] = toAddrs
		}
		
		// Cc
		if len(msg.Envelope.Cc) > 0 {
			ccAddrs := make([]string, 0, len(msg.Envelope.Cc))
			for _, addr := range msg.Envelope.Cc {
				if addr.MailboxName != "" && addr.HostName != "" {
					ccAddrs = append(ccAddrs, fmt.Sprintf("%s@%s", addr.MailboxName, addr.HostName))
				} else if addr.MailboxName != "" {
					ccAddrs = append(ccAddrs, addr.MailboxName)
				}
			}
			result["cc"] = ccAddrs
		}
		
		// Bcc
		if len(msg.Envelope.Bcc) > 0 {
			bccAddrs := make([]string, 0, len(msg.Envelope.Bcc))
			for _, addr := range msg.Envelope.Bcc {
				if addr.MailboxName != "" && addr.HostName != "" {
					bccAddrs = append(bccAddrs, fmt.Sprintf("%s@%s", addr.MailboxName, addr.HostName))
				} else if addr.MailboxName != "" {
					bccAddrs = append(bccAddrs, addr.MailboxName)
				}
			}
			result["bcc"] = bccAddrs
		}
		
		// Date (data di invio dal mittente)
		if !msg.Envelope.Date.IsZero() {
			result["date"] = msg.Envelope.Date.Format(time.RFC3339)
			result["dateTimestamp"] = msg.Envelope.Date.Unix()
		}
	}
	
	// InternalDate (data di arrivo sul server)
	if !msg.InternalDate.IsZero() {
		result["internalDate"] = msg.InternalDate.Format(time.RFC3339)
		result["internalDateTimestamp"] = msg.InternalDate.Unix()
	}
	
	// Body
	section, _ := imap.ParseBodySectionName("BODY[TEXT]")
	r := msg.GetBody(section)
	if r != nil {
		qr := quotedprintable.NewReader(r)
		bs, err := ioutil.ReadAll(qr)
		if err == nil {
			result["body"] = string(bs)
		}
	}
	
	// Headers (se disponibili)
	if len(msg.Body) > 0 {
		// Prova a recuperare gli headers dal body
		headerSection, _ := imap.ParseBodySectionName("BODY[HEADER]")
		headerReader := msg.GetBody(headerSection)
		if headerReader != nil {
			headerBytes, err := ioutil.ReadAll(headerReader)
			if err == nil {
				headers := make(map[string]interface{})
				headerText := string(headerBytes)
				lines := strings.Split(headerText, "\r\n")
				var currentKey string
				var currentValues []string
				
				for _, line := range lines {
					line = strings.TrimRight(line, "\r\n")
					if line == "" {
						// Fine degli headers
						break
					}
					if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
						// Continuazione della riga precedente
						if currentKey != "" && len(currentValues) > 0 {
							currentValues[len(currentValues)-1] += " " + strings.TrimSpace(line)
						}
					} else if strings.Contains(line, ":") {
						// Salva l'header precedente
						if currentKey != "" {
							existingValue, exists := headers[currentKey]
							if exists {
								// Header già presente, aggiungi ai valori esistenti
								if existingArray, ok := existingValue.([]string); ok {
									headers[currentKey] = append(existingArray, currentValues...)
								} else if existingStr, ok := existingValue.(string); ok {
									headers[currentKey] = []string{existingStr, currentValues[0]}
								}
							} else {
								// Nuovo header
								if len(currentValues) == 1 {
									headers[currentKey] = currentValues[0]
								} else {
									headers[currentKey] = currentValues
								}
							}
						}
						// Nuovo header
						parts := strings.SplitN(line, ":", 2)
						if len(parts) == 2 {
							currentKey = strings.TrimSpace(strings.ToLower(parts[0]))
							currentValues = []string{strings.TrimSpace(parts[1])}
						} else {
							currentKey = ""
							currentValues = nil
						}
					}
				}
				// Aggiungi l'ultimo header
				if currentKey != "" {
					if len(currentValues) == 1 {
						headers[currentKey] = currentValues[0]
					} else {
						headers[currentKey] = currentValues
					}
				}
				if len(headers) > 0 {
					result["headers"] = headers
				}
			}
		}
	}
	
	// Message ID (UID)
	result["uid"] = msg.SeqNum
	
	return result, nil
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

func (e *EmailClient) Read(headerObj map[string]interface{}) (map[string]interface{}, string) {
	fmt.Println("Read called with headerObj:", headerObj)
	
	// Verifica che il client sia connesso
	if e.client == nil {
		return nil, "Client not connected. Call login() first."
	}
	
	_, err := e.client.Select("INBOX", true)
	if err != nil {
		fmt.Printf("Error selecting INBOX: %v\n", err)
		return nil, err.Error()
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
		return nil, err.Error()
	}

	fmt.Printf("Found %d message IDs\n", len(ids))

	if len(ids) == 0 {
		return nil, "No messages found"
	}

	// Prendi solo il primo messaggio (il più recente, ultimo ID)
	latestID := ids[len(ids)-1]
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(latestID)

	// Recupera ENVELOPE (subject, from, to, date) e BODY[TEXT] (body)
	items := []imap.FetchItem{
		imap.FetchItem("ENVELOPE"),
		imap.FetchItem("BODY[TEXT]"),
		imap.FetchItem("BODY[HEADER]"),
	}
	messages := make(chan *imap.Message, 1)

	fmt.Printf("Fetching message ID %d...\n", latestID)
	err = e.client.Fetch(seqSet, items, messages)
	if err != nil {
		fmt.Printf("Error fetching: %v\n", err)
		return nil, err.Error()
	}

	fmt.Println("Fetch completed, reading from channel...")
	msg := <-messages
	fmt.Println("Message received from channel")
	
	if msg == nil {
		return nil, "No message"
	}

	emailMap, err := messageToMap(msg)
	if err != nil {
		fmt.Printf("Error converting message to map: %v\n", err)
		return nil, err.Error()
	}

	fmt.Println("Read successful")
	return emailMap, ""
}

func (e *EmailClient) WaitNewEmail(headerObj map[string]interface{}, timeoutMs int64) *sobek.Promise {
	// Verifica che il VU sia disponibile
	if e.Vu == nil {
		panic("VU context not available. EmailClient must be created inside the default function, not in init context.")
	}
	
	promise, resolve, reject := promises.New(e.Vu)
	
	// Verifica che il client sia connesso
	if e.client == nil {
		reject(fmt.Errorf("Client not connected. Call login() first."))
		return promise
	}
	
	go func() {
		fmt.Println("WaitNewEmail started, timeout:", timeoutMs, "ms")
		startTime := time.Now()
		// Sottrai 1 secondo per evitare problemi di precisione con il server IMAP
		searchSince := startTime.Add(-1 * time.Second)
		timeoutDuration := time.Duration(timeoutMs) * time.Millisecond
		
		// Converti l'oggetto JavaScript in textproto.MIMEHeader
		header := convertJSObjectToMIMEHeader(headerObj)
		
		// Polling ogni 2 secondi
		pollInterval := 2 * time.Second
		iteration := 0
		
		// Set di message ID già controllati e non validi (da skippare)
		skippedIDs := make(map[uint32]bool)
		
		for {
			iteration++
			elapsed := time.Since(startTime)
			
			// Controlla se il timeout è scaduto
			if elapsed >= timeoutDuration {
				fmt.Printf("WaitNewEmail timeout after %d iterations, elapsed: %v\n", iteration, elapsed)
				reject(fmt.Errorf("Timeout: no new email found within %d ms", timeoutMs))
				return
			}
			
			fmt.Printf("WaitNewEmail iteration %d, elapsed: %v\n", iteration, elapsed)
			
			// Seleziona la mailbox
			_, err := e.client.Select("INBOX", true)
			if err != nil {
				fmt.Printf("Error selecting INBOX: %v\n", err)
				reject(err)
				return
			}
			
			// Crea i criteri di ricerca con Since per filtrare solo email nuove
			// Since usa la "Internal date" (data di arrivo sul server)
			criteria := &imap.SearchCriteria{
				Header: header,
				Since:  searchSince,
			}
			
			ids, err := e.client.Search(criteria)
			if err != nil {
				fmt.Printf("Error searching: %v\n", err)
				reject(err)
				return
			}
			
			fmt.Printf("Found %d emails matching criteria (with Since filter)\n", len(ids))
			
			// Se troviamo email, controlla solo l'ultima (più recente)
			// perché quelle precedenti non ci servono
			if len(ids) > 0 {
				// Prendi solo l'ultimo ID (il più recente, dato che sono ordinati crescente)
				latestID := ids[len(ids)-1]
				
				// Se questo ID è già stato controllato e non era valido, skippalo
				if skippedIDs[latestID] {
					fmt.Printf("Skipping message ID %d (already checked and not valid)\n", latestID)
					time.Sleep(pollInterval)
					continue
				}
				
				seqSet := new(imap.SeqSet)
				seqSet.AddNum(latestID)
				
				// Recupera ENVELOPE, INTERNALDATE, BODY[TEXT] e BODY[HEADER]
				items := []imap.FetchItem{
					imap.FetchItem("ENVELOPE"),
					imap.FetchItem("INTERNALDATE"),
					imap.FetchItem("BODY[TEXT]"),
					imap.FetchItem("BODY[HEADER]"),
				}
				messages := make(chan *imap.Message, 1)
				
				fmt.Printf("Fetching latest message ID %d to check date...\n", latestID)
				err = e.client.Fetch(seqSet, items, messages)
				if err != nil {
					fmt.Printf("Error fetching message ID %d: %v\n", latestID, err)
					// Continua il polling se c'è un errore nel fetch
					time.Sleep(pollInterval)
					continue
				}
				
				msg := <-messages
				if msg == nil {
					fmt.Printf("Message ID %d is nil\n", latestID)
					// Aggiungi l'ID al set di skipped perché non è valido
					skippedIDs[latestID] = true
					// Continua il polling se il messaggio è nil
					time.Sleep(pollInterval)
					continue
				}
				
				// Verifica che la data interna (data di arrivo sul server) sia successiva a startTime
				if !msg.InternalDate.IsZero() {
					fmt.Printf("Message ID %d InternalDate: %v, startTime: %v, after: %v\n", 
						latestID, msg.InternalDate, startTime, msg.InternalDate.After(startTime))
					
					if msg.InternalDate.After(startTime) {
						// Questa è una nuova email, convertila in oggetto strutturato
						fmt.Printf("Found new email with ID %d\n", latestID)
						
						emailMap, err := messageToMap(msg)
						if err != nil {
							fmt.Printf("Error converting message to map: %v\n", err)
							reject(err)
							return
						}
						
						fmt.Printf("WaitNewEmail success after %d iterations\n", iteration)
						resolve(emailMap)
						return
					} else {
						// La data non è valida, aggiungi l'ID al set di skipped
						fmt.Printf("Message ID %d is not new (InternalDate not after startTime), adding to skipped list\n", latestID)
						skippedIDs[latestID] = true
					}
				} else {
					fmt.Printf("Message ID %d has no InternalDate\n", latestID)
					// Aggiungi l'ID al set di skipped perché non ha InternalDate
					skippedIDs[latestID] = true
				}
			}
			
			// Aspetta prima del prossimo polling
			// fmt.Printf("No new emails found, waiting %v before next check\n", pollInterval)
			time.Sleep(pollInterval)
		}
	}()
	
	return promise
}

// DeleteEmailsOlderThan elimina tutte le email più vecchie della data specificata
// La data viene confrontata con InternalDate (data di arrivo sul server)
// Restituisce il numero di email eliminate e un eventuale errore come stringa
// beforeTimestampUnix è un timestamp Unix in secondi (int64)
// Usage da JavaScript: client.DeleteEmailsOlderThan(Math.floor(Date.now() / 1000) - 86400) // 24 ore fa
func (e *EmailClient) DeleteEmailsOlderThan(beforeTimestampUnix int64) (int, string) {
	// Verifica che il client sia connesso
	if e.client == nil {
		return 0, "client not connected. Call login() first"
	}

	// Seleziona la mailbox INBOX in modalità read-write (false) per permettere l'eliminazione
	_, err := e.client.Select("INBOX", false)
	if err != nil {
		return 0, fmt.Sprintf("error selecting INBOX: %v", err)
	}

	// Converti il timestamp Unix in time.Time
	beforeDate := time.Unix(beforeTimestampUnix, 0)

	// Cerca tutte le email più vecchie della data specificata
	// Before usa la "Internal date" (data di arrivo sul server)
	criteria := &imap.SearchCriteria{
		Before: beforeDate,
	}

	ids, err := e.client.Search(criteria)
	if err != nil {
		return 0, fmt.Sprintf("error searching emails: %v", err)
	}

	if len(ids) == 0 {
		return 0, "" // Nessuna email da eliminare
	}

	// Crea un SeqSet con tutti gli ID trovati
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(ids...)

	// Marca le email come cancellate usando il flag \Deleted
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.DeletedFlag}
	err = e.client.Store(seqSet, item, flags, nil)
	if err != nil {
		return 0, fmt.Sprintf("error marking emails as deleted: %v", err)
	}

	// Rimuovi definitivamente le email marcate come cancellate
	err = e.client.Expunge(nil)
	if err != nil {
		return 0, fmt.Sprintf("error expunging emails: %v", err)
	}

	return len(ids), ""
}

func (e *EmailClient) Logout() {
	if e.client != nil {
		e.client.Logout()
	}
}
