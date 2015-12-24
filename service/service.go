package service

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"

	auth "github.com/abbot/go-http-auth"
	"github.com/foomo/petze/collector"
	"github.com/foomo/petze/config"
	"github.com/julienschmidt/httprouter"
)

func jsonReply(data interface{}, w http.ResponseWriter) error {
	jsonBytes, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonBytes)
	log.Println("sent json reply:", len(jsonBytes))
	return nil
}

func errReply(w http.ResponseWriter, code int, err error) {
	log.Println("an error occurred", code, err.Error())
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(code)
	w.Write([]byte(err.Error()))
}

func extractJSONBodyIntoData(r *http.Request, data interface{}) error {
	jsonBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(jsonBytes) == 0 {
		return errors.New("body was empty")
	}
	err = json.Unmarshal(jsonBytes, &data)
	if err != nil {
		return err
	}
	return nil
}

type server struct {
	router    *httprouter.Router
	collector *collector.Collector
}

func newServer(servicesConfigfile string, peopleConfigfile string) (s *server, err error) {
	coll, err := collector.NewCollector(servicesConfigfile, peopleConfigfile)
	s = &server{
		router:    httprouter.New(),
		collector: coll,
	}
	s.router.GET("/collector/config/services", s.GETCollectorConfigServices)
	return s, nil
}

type basicAuthHandler struct {
	server        *server
	authenticator *auth.BasicAuth
}

func newBasicAuthHandler(server *server, htpasswordFile string) (ba *basicAuthHandler) {
	secretProvider := auth.HtpasswdFileProvider(htpasswordFile)
	authenticator := auth.NewBasicAuthenticator("dumpster", secretProvider)
	return &basicAuthHandler{
		server:        server,
		authenticator: authenticator,
	}
}

func (ba *basicAuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user := ba.authenticator.CheckAuth(r)
	if len(user) == 0 {
		ba.authenticator.RequireAuth(w, r)
		return
	}
	ba.server.router.ServeHTTP(w, r)
}

func getTLSConfig() *tls.Config {
	c := &tls.Config{}
	c.MinVersion = tls.VersionTLS12
	c.PreferServerCipherSuites = true
	c.CipherSuites = []uint16{
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
	}
	c.CurvePreferences = []tls.CurveID{
		tls.CurveP256,
		tls.CurveP384,
		tls.CurveP521,
	}
	return c
}

// Run as a server
func Run(c *config.Server, servicesConfigfile string, peopleConfigfile string) error {
	s, err := newServer(servicesConfigfile, peopleConfigfile)
	if err != nil {
		return err
	}
	log.Println("starting petze server on: ", c.Address)
	log.Println("  basic auth from: ", c.BasicAuthFile)
	ba := newBasicAuthHandler(s, c.BasicAuthFile)
	errorChan := make(chan (error))
	if len(c.Address) > 0 {
		go func() {
			errorChan <- http.ListenAndServe(c.Address, ba)
		}()
	}
	if c.TLS != nil {
		go func() {
			log.Println("tls is configured: ", c.TLS)
			tlsServer := &http.Server{
				Addr:      c.Address,
				Handler:   ba,
				TLSConfig: getTLSConfig(),
			}
			errorChan <- tlsServer.ListenAndServeTLS(c.TLS.Cert, c.TLS.Key)
		}()
	}
	return <-errorChan
}