package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
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

func encryptPassword(username, password, random, realm string) string {
	h1 := md5.New()
	b1 := []byte(fmt.Sprintf("%s:%s:%s", username, realm, password))
	h1.Write(b1)
	b2 := []byte(fmt.Sprintf("%s:%s:%s", username, random, fmt.Sprintf("%X", h1.Sum(nil))))
	h1.Reset()
	h1.Write(b2)
	return fmt.Sprintf("%X", h1.Sum(nil))
}

func (a *amcrest) login() {

	// First request, to get the random bits
	data := map[string]interface{}{"params": map[string]interface{}{"userName": a.username, "password": "", "clientType": "Web3.0", "loginType": "Direct"}, "id": a.id, "method": "global.login"}
	json_data, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}

	resp, err := http.Post(fmt.Sprintf("%s/RPC2_Login", a.host), "application/x-www-form-urlencoded", bytes.NewBuffer(json_data))
	a.id++
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
	rand := fmt.Sprintf("%v", params["random"])
	realm := fmt.Sprintf("%v", params["realm"])
	session := fmt.Sprintf("%v", result["session"])

	hash := encryptPassword(a.username, a.password, rand, realm)

	// second request, actual login
	data = map[string]interface{}{"params": map[string]interface{}{"userName": a.username, "password": hash, "clientType": "Web3.0", "loginType": "Direct"}, "id": a.id, "method": "global.login", "session": session}
	json_data, err = json.Marshal(data)
	if err != nil {
		panic(err)
	}

	resp2, err := http.Post(fmt.Sprintf("%s/RPC2_Login", a.host), "application/x-www-form-urlencoded", bytes.NewBuffer(json_data))
	a.id++
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
	a.session = fmt.Sprintf("%v", result2["session"])
}

func (a *amcrest) sendKeepAlive() {
	ticker := time.NewTicker(50 * time.Second)
	for range ticker.C {
		if a.session == "" {
			return
		}

		resp := a.rcpPost("/RPC2", map[string]interface{}{
			"method":  "global.keepAlive",
			"params":  map[string]interface{}{"timeout": 300, "active": true},
			"id":      a.id,
			"session": a.session,
		})
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		fmt.Println(string(body))
	}
}

func (a *amcrest) rcpPost(path string, data map[string]interface{}) *http.Response {
	json_data, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	resp, err := http.Post(fmt.Sprintf("%s%s", a.host, path), "application/x-www-form-urlencoded", bytes.NewBuffer(json_data))
	a.id++
	if err != nil {
		panic(err)
	}
	return resp
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
				handler(fmt.Sprintf("%v", e))
			}
		}
	}
}

type event struct {
	method string
	action string
	code   string
	time   string
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

func (t *telegram) telegramHandler(msg string) {
	http.Get(fmt.Sprintf("https://api.telegram.org/%s/sendMessage?chat_id=%s&text=%s", t.bot_key, t.chat_id, msg))
}

func main() {
	var cam = amcrest{"http://" /*FIXME*/, "admin", "" /*FIXME*/, "", 2}

	cam.login()
	fmt.Println(cam)
	go cam.sendKeepAlive()
	var tel = telegram{"" /* FIXME */, "" /*FIXME*/}
	cam.watchAlarms(tel.telegramHandler)
}
