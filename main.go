package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime/multipart"
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

const nonceBytes = "abcdef0123456789"

type amcrest struct {
	host     string
	username string
	password string
	session  string
	id       int
	cache    cache
	timezone *time.Location
	name     string
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

type cache struct {
	ProcessedFiles map[string]time.Time `json:"processed_files"`
}

func saveCache(c *cache, cachePath string) {
	if cachePath == "" {
		return
	}
	now := time.Now()
	filtered := make(map[string]time.Time)
	for filename, t := range c.ProcessedFiles {
		if now.Sub(t) < 24*time.Hour {
			filtered[filename] = t
		}
	}
	c.ProcessedFiles = filtered

	file, err := os.Create(cachePath)
	if err != nil {
		log.Printf("Error creating cache file: %v", err)
		return
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	if err := enc.Encode(c); err != nil {
		log.Printf("Error encoding cache to JSON: %v", err)
		return
	}
	if err := file.Sync(); err != nil {
		log.Printf("Error syncing cache file: %v", err)
		return
	}
	log.Printf("Cache saved to %s", cachePath)
}

// Load cache from JSON, keeping only files younger than 24h
func loadCache(cachePath string) cache {
	c := cache{ProcessedFiles: make(map[string]time.Time)}
	if cachePath == "" {
		return c
	}
	if fi, err := os.Stat(cachePath); err == nil && !fi.IsDir() {
		file, err := os.Open(cachePath)
		if err != nil {
			log.Printf("Error opening cache file: %v", err)
			return c
		}
		defer file.Close()
		dec := json.NewDecoder(file)
		if err := dec.Decode(&c); err != nil {
			log.Printf("Error decoding cache JSON: %v", err)
			return c
		}
		now := time.Now()
		filtered := make(map[string]time.Time)
		for filename, t := range c.ProcessedFiles {
			if now.Sub(t) < 24*time.Hour {
				filtered[filename] = t
			}
		}
		c.ProcessedFiles = filtered
	} else {
		log.Printf("Cache file %s does not exist or is a directory, starting with an empty cache", cachePath)
	}
	return c
}

func getNewAmcrest() *amcrest {
	tz, err := time.LoadLocation(getEnv("AMCREST_TIMEZONE", "UTC"))
	if err != nil {
		panic(err)
	}

	var cachePath = getEnv("AMCREST_CACHE_PATH", "")
	if cachePath == "" {
		panic("AMCREST_CACHE_PATH is not set")
	}

	a := amcrest{
		getEnv("AMCREST_BASEURL", ""),
		getEnv("AMCREST_USER", "admin"),
		getEnv("AMCREST_PASSWORD", ""),
		"",
		2,
		loadCache(cachePath),
		tz,
		getEnv("AMCREST_NAME", "Camera"),
	}

	return &a
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

func (a *amcrest) rcpPost(path string, data map[string]any) (*http.Response, error) {
	data["id"] = a.id
	data["session"] = a.session
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

	resp, err := a.rcpPost("/RPC2", map[string]any{
		"method": "global.setCurrentTime",
		"params": map[string]any{"time": localtime, "tolerance": 5},
	})
	if err != nil {
		panic(err)
	}

	defer resp.Body.Close()
	var result map[string]any
	body, err := io.ReadAll(resp.Body)
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
	if a.username == "" || a.password == "" || a.host == "" {
		panic("Login, password or host is not set")
	}

	log.Println("Logging in")
	// First request, to get the random bits
	resp, err := a.rcpPost("/RPC2_Login", map[string]any{
		"method": "global.login",
		"params": map[string]any{"userName": a.username, "password": "", "clientType": "Web3.0", "loginType": "Direct"},
	})

	if err != nil {
		panic(err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	var result map[string]any
	json.Unmarshal(body, &result)
	params := result["params"].(map[string]any)
	a.session = result["session"].(string)

	if a.session == "" {
		panic("Session is empty, login failed")
	}

	hash := encryptPassword(a.username, a.password, params["random"].(string), params["realm"].(string))

	// second request, actual login
	resp2, err := a.rcpPost("/RPC2_Login", map[string]any{
		"method": "global.login",
		"params": map[string]any{"userName": a.username, "password": hash, "clientType": "Web3.0", "loginType": "Direct"},
	})

	if err != nil {
		panic(err)
	}

	defer resp2.Body.Close()
	var result2 map[string]any
	body, err = io.ReadAll(resp2.Body)
	if err != nil {
		panic(err)
	}
	json.Unmarshal(body, &result2)
	if !result2["result"].(bool) {
		panic("Log in unsuccessful")
	}
	fmt.Println("Log in successful!")
}

func (a *amcrest) getFileFindObject() (int, error) {
	resp, err := a.rcpPost("/RPC2", map[string]any{
		"method": "mediaFileFind.factory.create",
		"params": nil,
	})

	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var result map[string]any
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	json.Unmarshal(body, &result)

	return int(result["result"].(float64)), nil
}

// This also selects the timeframe for the next request
func (a *amcrest) hasFindFile(mediaFileFindFactory int, startTime string, endTime string) bool {
	resp, err := a.rcpPost("/RPC2", map[string]any{
		"method": "mediaFileFind.findFile",
		"params": map[string]any{
			"condition": map[string]any{
				"Channel":   0,
				"Dirs":      []string{"/mnt/sd"},
				"Types":     []string{"mp4"},
				"Order":     "Ascent",
				"Redundant": "Exclusion",
				"Events":    nil,
				"StartTime": startTime,
				"EndTime":   endTime,
				"Flags":     []string{"Timing", "Event", "Event", "Manual"},
			},
		},
		"object": mediaFileFindFactory,
	})

	if err != nil {
		log.Println("Error finding file:", err)
		return false
	}

	defer resp.Body.Close()

	var result map[string]any
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	json.Unmarshal(body, &result)
	return result["result"].(bool)
}

func (a *amcrest) getLatestFile(handler func(telegramMessage)) {
	startDate := time.Now().Add(-12 * time.Hour).In(a.timezone).Format("2006-01-02 15:04:05")
	endDate := time.Now().Add(12 * time.Hour).In(a.timezone).Format("2006-01-02 15:04:05")

	mediaFileFindFactory, err := a.getFileFindObject()
	if err != nil {
		log.Println("Error getting mediaFileFindFactory:", err)
		return
	}

	if !a.hasFindFile(mediaFileFindFactory, startDate, endDate) {
		log.Println("No files found")
		return
	}

	resp, err := a.rcpPost("/RPC2", map[string]any{
		"method": "mediaFileFind.findNextFile",
		"params": map[string]any{
			"count": 100,
		},
		"object": mediaFileFindFactory,
	})

	if err != nil {
		log.Println("Error finding next file:", err)
		return
	}

	defer resp.Body.Close()

	var result map[string]any
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	json.Unmarshal(body, &result)

	if result["result"].(bool) && result["params"].(map[string]any)["found"].(float64) > 0 {
		for _, recording := range result["params"].(map[string]any)["infos"].([]any) {
			path := recording.(map[string]any)["FilePath"].(string)
			if _, ok := a.cache.ProcessedFiles[path]; !ok {
				a.cache.ProcessedFiles[path] = time.Now()
				file, err := a.downloadVideo(path)
				if err != nil {
					log.Printf("Error downloading video: %v", err)
					continue
				}

				handler(telegramMessage{
					messageType: Video,
					text:        parseVideoFilePath(path),
					filepath:    file,
				})
				log.Printf("sent %s\n", file)
				os.Remove(file)
				saveCache(&a.cache, getEnv("AMCREST_CACHE_PATH", ""))
			}
		}
	}
}

func parseWwwAuthenticate(header string) (string, string, string) {
	r, _ := regexp.Compile("Digest realm=\"([^\"]+)\", qop=\"auth\", nonce=\"([^\"]+)\", opaque=\"([^\"]+)\"")
	matches := r.FindStringSubmatch(string(header))
	return matches[1], matches[2], matches[3]
}

func (a *amcrest) downloadVideo(videopath string) (string, error) {
	file, err := os.CreateTemp("/tmp", "*.mp4")
	if err != nil {
		return "", err
	}

	defer file.Close()

	client := &http.Client{}
	uri := fmt.Sprintf("/cgi-bin/RPC_Loadfile%s", videopath)

	req, err := http.NewRequest("GET", fmt.Sprintf("%s%s", a.host, uri), nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	realm, nonce, opaque := parseWwwAuthenticate(resp.Header["Www-Authenticate"][0])
	cnonce, challresp := a.authChallenge(uri, realm, nonce)

	req, err = http.NewRequest("GET", fmt.Sprintf("%s%s", a.host, uri), nil)
	if err != nil {
		return "", err
	}
	authstr := fmt.Sprintf(
		"Digest username=\"%s\", realm=\"%s\", nonce=\"%s\", uri=\"%s\", response=\"%s\", opaque=\"%s\", qop=auth, nc=%s, cnonce=\"%s\"",
		a.username, realm, nonce, uri, challresp, opaque, "00000001", cnonce)
	req.Header.Add("Authorization", authstr)

	resp, err = client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return "", err
	}
	log.Printf("downloaded %s as %s", videopath, file.Name())
	return file.Name(), nil
}

func (a *amcrest) sendKeepAlive() {
	ticker := time.NewTicker(55 * time.Second)
	for range ticker.C {
		if a.session == "" {
			return
		}

		resp, err := a.rcpPost("/RPC2", map[string]any{
			"method": "global.keepAlive",
			"params": map[string]any{"timeout": 300, "active": true},
		})

		if err != nil {
			log.Printf("Error sending keepalive: %v\n", err)
		}
		a.setDeviceTime()

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		var result map[string]any
		if err != nil {
			log.Printf("Error reading keepalive response: %v\n", err)
		} else {
			json.Unmarshal(body, &result)
			log.Printf("Keepalive sent %v\n", result)
			if !result["result"].(bool) {
				a.login()
			}
		}
	}
}

func (a *amcrest) watchAlarms(handler func(telegramMessage)) {

	// Instanciate event factory FIXME: useless?
	a.rcpPost("/RPC2", map[string]any{
		"method": "eventManager.factory.instance",
		"params": nil,
	})

	// subscribe to videomotion
	a.rcpPost("/RPC2", map[string]any{
		"method": "eventManager.attach",
		"params": map[string]any{"codes": []string{"VideoMotion"}},
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
			log.Printf("Error reading event stream: %s", err)
			time.Sleep(time.Second * 5)
			continue
		}
		matches := r.FindStringSubmatch(string(buf))
		if len(matches) == 2 {
			events := parseEvent([]byte(r.FindStringSubmatch(string(buf))[1]))
			for _, e := range events {
				log.Printf("event: %v", e)
				if !e.Equals(last_event) {
					handler(telegramMessage{
						messageType: Text,
						text:        fmt.Sprintf("%s: %s %s %v", a.name, e.code, e.action, e.time),
					})
					last_event = e
				} else {
					log.Printf("Duplicate event")
				}
			}
		}
	}
}

func (a *amcrest) pollRecordingFiles(handler func(telegramMessage)) {
	a.getLatestFile(handler)
	ticker := time.NewTicker(60 * time.Second)
	for range ticker.C {
		log.Println("Polling recording files")
		if a.session == "" {
			return
		}
		a.getLatestFile(handler)
	}
}

func parseVideoFilePath(path string) string {
	r, err := regexp.Compile(`^/[^/]+/[^/]+/(\d{4}-\d{2}-\d{2})/(\d{3})/dav/(\d{2})/(\d{2}\.\d{2}\.\d{2})-(\d{2}\.\d{2}\.\d{2})\[(.)\]\[(\d)@(\d)\]\[(\d)\].mp4$`)
	if err != nil {
		log.Printf("Error compiling regex: %v", err)
		return path
	}
	matches := r.FindStringSubmatch(path)
	if len(matches) < 6 {
		log.Printf("Error parsing video file path: %s %v", path, matches)
		return path
	}
	date := matches[1]
	timeStart := matches[4]
	timeEnd := matches[5]
	return fmt.Sprintf("%s - %s to %s", date, timeStart, timeEnd)
}

func parseEvent(msg []byte) []event {
	var result map[string]any
	json.Unmarshal(msg, &result)

	params := result["params"].(map[string]any)
	eventlist := params["eventList"].([]any)

	events := make([]event, len(eventlist))
	for i, evt := range eventlist {
		e := event{}
		e.method = result["method"].(string)
		e.action = evt.(map[string]any)["Action"].(string)
		e.code = evt.(map[string]any)["Code"].(string)
		data := evt.(map[string]any)["Data"].(map[string]any)
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
		log.Println(err)
		return "", nil, err
	}
	defer file.Close()
	formfile, err := mp.CreateFormFile("video", "video.mp4")
	if err != nil {
		log.Println(err)
		return "", nil, err
	}
	io.Copy(formfile, file)
	return mp.FormDataContentType(), body, nil
}

type telegramMessage struct {
	messageType telegramMessageType
	text        string
	filepath    string
}

func (t *telegram) telegramHandler(m telegramMessage) {
	if t == nil {
		log.Printf("no Telegram bot configured, skipping message %v: %s %s\n", m.messageType, m.text, m.filepath)
		return
	}
	if m.messageType == Text {
		resp, err := http.Get(fmt.Sprintf("https://api.telegram.org/%s/sendMessage?chat_id=%s&text=%s", t.bot_key, t.chat_id, m.text))
		log.Printf("GET api.telegram.org message %d\n", resp.StatusCode)
		if err != nil {
			log.Printf("Telegram error: %v\n", err)
		}
	} else if m.messageType == Video {
		ct, body, err := createVideoForm(m.filepath)
		if err != nil {
			log.Println(err)
		}
		url := fmt.Sprintf("https://api.telegram.org/%s/sendVideo?chat_id=%s", t.bot_key, t.chat_id)
		resp, err := http.Post(url, ct, body)
		log.Printf("POST api.telegram.org video: %d\n", resp.StatusCode)
		if err != nil {
			log.Printf("Telegram error: %v\n", err)
		}
	}
}

func getEnv(key string, default_val string) string {
	val := os.Getenv(key)
	if val == "" {
		return default_val
	}
	return val
}

func main() {
	var cam = getNewAmcrest()

	cam.login()
	cam.setDeviceTime()

	go cam.sendKeepAlive()

	var tel *telegram

	if getEnv("TELEGRAM_BOT_KEY", "") != "" && getEnv("TELEGRAM_CHAT_ID", "") != "" {
		tel = &telegram{
			getEnv("TELEGRAM_BOT_KEY", ""),
			getEnv("TELEGRAM_CHAT_ID", ""),
		}
	}

	go cam.pollRecordingFiles(tel.telegramHandler)
	cam.watchAlarms(tel.telegramHandler)
}
