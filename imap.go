package imap

import (
	"errors"
	"io/ioutil"
	"mime/quotedprintable"
	"net/textproto"
	"strconv"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/grafana/sobek"

	ec "github.com/PaoloLeggio/xk6-imap/client"
	"go.k6.io/k6/js/common"
	"go.k6.io/k6/js/modules"
)

type (
	// RootModule is the global module instance that will create ModuleInstance
	// instances for each VU.
	RootModule struct{}

	// ModuleInstance represents an instance of the JS module.
	ModuleInstance struct {
		vu modules.VU
	}
)

// Ensure the interfaces are implemented correctly
var (
	_ modules.Instance = &ModuleInstance{}
	_ modules.Module   = &RootModule{}
)

func init() {
	modules.Register("k6/x/imap", new(RootModule))
}

// New returns a pointer to a new RootModule instance
func New() *RootModule {
	return &RootModule{}
}

// NewModuleInstance implements the modules.Module interface and returns
// a new instance for each VU.
func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	return &ModuleInstance{vu: vu}
}

// Exports implements the modules.Instance interface and returns
// the exports of the JS module.
// Creiamo esplicitamente un oggetto default che contiene Client come proprietà
// Questo permette "import Imap from 'k6/x/imap'" e poi "new Imap.Client(...)"
func (mi *ModuleInstance) Exports() modules.Exports {
	rt := mi.vu.Runtime()
	
	// Crea un oggetto JavaScript che contiene Client come proprietà
	exportsObj := rt.NewObject()
	
	// Wrappa la funzione EmailClient come costruttore JavaScript
	// Usa ToValue per convertire la funzione Go in un valore sobek
	clientConstructor := rt.ToValue(mi.EmailClient)
	exportsObj.Set("Client", clientConstructor)
	
	return modules.Exports{
		Default: exportsObj,
		Named: map[string]interface{}{
			"Client": mi.EmailClient,
		},
	}
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

// Simple function for one time read
// Use EmailClient for more complex needs
func (mi *ModuleInstance) Read(email, password, URL string, port int, headerObj map[string]interface{}) (string, string) {
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

// EmailClient is the JS constructor for the email client.
// It accepts email, password, url, and port as arguments.
// Usage: const client = new Imap.Client(email, password, url, port);
func (mi *ModuleInstance) EmailClient(call sobek.ConstructorCall) *sobek.Object {
	rt := mi.vu.Runtime()

	if len(call.Arguments) != 4 {
		common.Throw(rt, errors.New("Client requires 4 arguments: email, password, url, port"))
		return nil
	}

	// Estrai gli argomenti
	email, ok := call.Arguments[0].Export().(string)
	if !ok {
		common.Throw(rt, errors.New("first argument (email) must be a string"))
		return nil
	}

	password, ok := call.Arguments[1].Export().(string)
	if !ok {
		common.Throw(rt, errors.New("second argument (password) must be a string"))
		return nil
	}

	url, ok := call.Arguments[2].Export().(string)
	if !ok {
		common.Throw(rt, errors.New("third argument (url) must be a string"))
		return nil
	}

	// Gestisci sia int che float64 per il port
	var portInt int
	switch v := call.Arguments[3].Export().(type) {
	case float64:
		portInt = int(v)
	case int:
		portInt = v
	case int64:
		portInt = int(v)
	default:
		common.Throw(rt, errors.New("fourth argument (port) must be a number"))
		return nil
	}

	client := &ec.EmailClient{
		Vu:       mi.vu,
		Email:    email,
		Password: password,
		Url:      url,
		Port:     portInt,
	}

	return rt.ToValue(client).ToObject(rt)
}
