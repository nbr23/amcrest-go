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

type telegramMessageType int

const (
	Text telegramMessageType = iota
	Video
)

type amcrest struct {
	host       string
	username   string
	password   string
	session    string
	id         int
	videocache map[string]bool
	timezone   *time.Location
	name       string
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

func getCnonce() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = nonceBytes[rand.Int63()%int64(len(nonceBytes))]
	}
	return string(b)
}

func (a *amcrest) authChallenge(uri, realm, nonce string) (string, string) {
	h := md5.New()
	ba1 := []byte(fmt.Sprintf("%s:%s:%s", a.username, realm, a.password))
	h.Write(ba1)
	ha1 := fmt.Sprintf("%x", h.Sum(nil))

	h.Reset()
	ba2 := []byte(fmt.Sprintf("GET:%s", uri))
	h.Write(ba2)
	ha2 := fmt.Sprintf("%x", h.Sum(nil))

	h.Reset()
	cnonce := getCnonce()
	bresp := []byte(fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, nonce, "00000001", cnonce, "auth", ha2))
	h.Write(bresp)

	return cnonce, fmt.Sprintf("%x", h.Sum(nil))
}

func (a *amcrest) rcpPost(path string, data map[string]interface{}) (*http.Response, error) {
	json_data, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(fmt.Sprintf("%s%s", a.host, path), "application/x-www-form-urlencoded", bytes.NewBuffer(json_data))
	a.id++
	return resp, err
}

func (a *amcrest) setDeviceTime() {
	localtime := time.Now().In(a.timezone).Format("2006-01-02 15:04:05")

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
			fmt.Println(err)
		} else {
			log.Print("Keepalive sent")
		}
	}
}

func (a *amcrest) watchAlarms(handler func(telegramMessageType, string)) {

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
					handler(Text, fmt.Sprintf("%s: %v", a.name, e))
					a.getLatestFile(handler)
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

func createVideoForm(filepath string) (string, io.Reader, error) {
	body := new(bytes.Buffer)
	mp := multipart.NewWriter(body)
	defer mp.Close()
	file, err := os.Open(filepath)
	if err != nil {
		fmt.Println(err)
		return "", nil, err
	}
	defer file.Close()
	formfile, err := mp.CreateFormFile("video", "video.mp4")
	if err != nil {
		fmt.Println(err)
		return "", nil, err
	}
	io.Copy(formfile, file)
	return mp.FormDataContentType(), body, nil
}

func (t *telegram) telegramHandler(messageType telegramMessageType, msg string) {
	if messageType == Text {
		http.Get(fmt.Sprintf("https://api.telegram.org/%s/sendMessage?chat_id=%s&text=%s", t.bot_key, t.chat_id, msg))
	} else if messageType == Video {
		ct, body, err := createVideoForm(msg)
		if err != nil {
			fmt.Println(err)
		}
		url := fmt.Sprintf("https://api.telegram.org/%s/sendVideo?chat_id=%s", t.bot_key, t.chat_id)
		http.Post(url, ct, body)
	}
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
	tz, err := time.LoadLocation(getEnv("AMCREST_TIMEZONE", "UTC"))
	if err != nil {
		panic(err)
	}
	var cam = amcrest{
		getEnv("AMCREST_BASEURL", ""),
		getEnv("AMCREST_USER", "admin"),
		getEnv("AMCREST_PASSWORD", ""),
		"",
		2,
		map[string]bool{},
		tz,
		getEnv("AMCREST_NAME", "Camera"),
	}

	cam.login()
	cam.setDeviceTime()

	go cam.sendKeepAlive()

	var tel = telegram{
		getEnv("TELEGRAM_BOT_KEY", ""),
		getEnv("TELEGRAM_CHAT_ID", ""),
	}

	cam.watchAlarms(tel.telegramHandler)
}
