package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"
)

type amcrest struct {
	host     string
	username string
	password string
	session  string
	id       int
}

type telegram struct {
	bot_key string
	chat_id string
}

type event struct {
	method string
	action string
	code   string
	time   string
}

func encryptPassword(username, password, random, realm string) string {
	h1 := md5.New()
	b1 := []byte(fmt.Sprintf("%s:%s:%s", username, realm, password))
	h1.Write(b1)
	b2 := []byte(fmt.Sprintf("%s:%s:%s", username, random, fmt.Sprintf("%X", h1.Sum(nil))))
	h1.Reset()
	h1.Write(b2)
	return fmt.Sprintf("%X", h1.Sum(nil))
}

func (a *amcrest) rcpPost(path string, data map[string]interface{}) (*http.Response, error) {
	json_data, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	resp, err := http.Post(fmt.Sprintf("%s%s", a.host, path), "application/x-www-form-urlencoded", bytes.NewBuffer(json_data))
	a.id++
	return resp, err
}

func (a *amcrest) setDeviceTime(timezone string) {
	var localtime string
	if timezone != "" {
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			panic(err)
		}
		localtime = time.Now().In(loc).Format("2006-01-02 15:04:05")
	} else {
		localtime = time.Now().Format("2006-01-02 15:04:05")
	}
	resp, err := a.rcpPost("/RPC2", map[string]interface{}{
		"method":  "global.setCurrentTime",
		"params":  map[string]interface{}{"time": localtime, "tolerance": 5},
		"id":      a.id,
		"session": a.session,
	})
	if err != nil {
		panic(err)
	}

	defer resp.Body.Close()
	var result map[string]interface{}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	json.Unmarshal(body, &result)
	if !result["result"].(bool) {
		panic("Setting time unsuccessful")
	}
	log.Printf("Time set to %s\n", localtime)
}

func (a *amcrest) login() {

	// First request, to get the random bits
	resp, err := a.rcpPost("/RPC2_Login", map[string]interface{}{
		"method":  "global.login",
		"params":  map[string]interface{}{"userName": a.username, "password": "", "clientType": "Web3.0", "loginType": "Direct"},
		"id":      a.id,
		"session": a.session,
	})

	if err != nil {
		panic(err)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)
	params := result["params"].(map[string]interface{})
	a.session = result["session"].(string)

	hash := encryptPassword(a.username, a.password, params["random"].(string), params["realm"].(string))

	// second request, actual login
	resp2, err := a.rcpPost("/RPC2_Login", map[string]interface{}{
		"method":  "global.login",
		"params":  map[string]interface{}{"userName": a.username, "password": hash, "clientType": "Web3.0", "loginType": "Direct"},
		"id":      a.id,
		"session": a.session,
	})

	if err != nil {
		panic(err)
	}

	defer resp2.Body.Close()
	var result2 map[string]interface{}
	body, err = ioutil.ReadAll(resp2.Body)
	if err != nil {
		panic(err)
	}
	json.Unmarshal(body, &result2)
	if !result2["result"].(bool) {
		panic("Log in unsuccessful")
	}
}

func (a *amcrest) sendKeepAlive() {
	ticker := time.NewTicker(120 * time.Second)
	for range ticker.C {
		if a.session == "" {
			return
		}

		resp, err := a.rcpPost("/RPC2", map[string]interface{}{
			"method":  "global.keepAlive",
			"params":  map[string]interface{}{"timeout": 300, "active": true},
			"id":      a.id,
			"session": a.session,
		})

		if err != nil {
			log.Println(err)
		}

		defer resp.Body.Close()
		_, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		log.Print("Keepalive sent")
	}
}

func (a *amcrest) watchAlarms(handler func(string)) {

	// Instanciate event factory FIXME: useless?
	a.rcpPost("/RPC2", map[string]interface{}{
		"method":  "eventManager.factory.instance",
		"params":  nil,
		"id":      a.id,
		"session": a.session,
	})

	// subscribe to videomotion
	a.rcpPost("/RPC2", map[string]interface{}{
		"method":  "eventManager.attach",
		"params":  map[string]interface{}{"codes": []string{"VideoMotion"}},
		"id":      a.id,
		"session": a.session,
	})

	// Open stream
	client := &http.Client{}
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/SubscribeNotify.cgi?sessionId=%s", a.host, a.session), nil)
	req.Header.Add("Cookie", fmt.Sprintf("secure; DhWebClientSessionID=%s; username=%s", a.session, a.username))
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	r, _ := regexp.Compile("var json=({.*})\n")
	reader := bufio.NewReader(resp.Body)
	buf := make([]byte, 1024)
	last_event := event{}
	for {
		_, err := reader.Read(buf)
		if err != nil {
			panic(err)
		}
		matches := r.FindStringSubmatch(string(buf))
		if len(matches) == 2 {
			events := parseEvent([]byte(r.FindStringSubmatch(string(buf))[1]))
			for _, e := range events {
				fmt.Println(e)
				if !e.Equals(last_event) {
					handler(fmt.Sprintf("%v", e))
					last_event = e
				} else {
					log.Printf("Duplicate event")
				}
			}
		}
	}
}

func parseEvent(msg []byte) []event {
	var result map[string]interface{}
	json.Unmarshal(msg, &result)

	params := result["params"].(map[string]interface{})
	eventlist := params["eventList"].([]interface{})

	events := make([]event, len(eventlist))
	for i, evt := range eventlist {
		e := event{}
		e.method = result["method"].(string)
		e.action = evt.(map[string]interface{})["Action"].(string)
		e.code = evt.(map[string]interface{})["Code"].(string)
		data := evt.(map[string]interface{})["Data"].(map[string]interface{})
		e.time = data["LocaleTime"].(string)
		events[i] = e
	}
	return events
}

func (e event) Equals(e2 event) bool {
	return e.action == e2.action && e.code == e2.code && e.method == e2.method && e.time == e2.time
}

func (t *telegram) telegramHandler(msg string) {
	http.Get(fmt.Sprintf("https://api.telegram.org/%s/sendMessage?chat_id=%s&text=%s", t.bot_key, t.chat_id, msg))
}

func getEnv(key string, default_val string) string {
	val := os.Getenv(key)
	if val == "" {
		if default_val == "" {
			panic(fmt.Errorf("%s is not set", key))
		}
		return default_val
	}
	return val
}

func main() {
	var cam = amcrest{
		getEnv("AMCREST_BASEURL", ""),
		getEnv("AMCREST_USER", "admin"),
		getEnv("AMCREST_PASSWORD", ""),
		"",
		2,
	}

	cam.login()
	cam.setDeviceTime(getEnv("AMCREST_TIMEZONE", "UTC"))

	go cam.sendKeepAlive()
	var tel = telegram{
		getEnv("TELEGRAM_BOT_KEY", ""),
		getEnv("TELEGRAM_CHAT_ID", ""),
	}
	cam.watchAlarms(tel.telegramHandler)
}
