package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

var classiID = os.Getenv("CLASSI_ID")
var classiPW = os.Getenv("CLASSI_PASSWORD")
var enablePost = envBool("CLASSI_ENABLE_POST", false)
var enableStudy = envBool("CLASSI_ENABLE_STUDY", true)
var enableCal = envBool("CLASSI_ENABLE_CALENDAR", true)
var enableLearningReport = envBool("CLASSI_ENABLE_LEARNING_REPORT", true)
var downloadDir = envStr("CLASSI_DOWNLOAD_DIR", "/root/.hermes/downloads/classi")
var subjectNames = map[int]string{34: "国語", 36: "社会", 38: "数学", 40: "理科", 42: "英語", 43: "その他", 57: "読書"}

func envBool(k string, d bool) bool { v := os.Getenv(k); if v == "" { return d }; b, _ := strconv.ParseBool(v); return b }
func envStr(k, d string) string { v := os.Getenv(k); if v == "" { return d }; return v }

type ClassiClient struct {
	resty    *resty.Client
	loggedIn bool
}

func NewClient() *ClassiClient {
	j, _ := cookiejar.New(nil)
	return &ClassiClient{resty: resty.New().SetCookieJar(j).SetHeader("User-Agent", "Mozilla/5.0").SetHeader("Accept", "application/json, text/plain, */*").SetTimeout(30 * time.Second)}
}

func (c *ClassiClient) Login(uid, pw string) error {
	if uid == "" { uid = classiID }
	if pw == "" { pw = classiPW }
	if uid == "" || pw == "" { return fmt.Errorf("missing env") }
	c.resty.R().Get("https://id.classi.jp/login/identifier")
	var csrf struct{ Data string }
	c.resty.R().SetResult(&csrf).Get("https://id-api.classi.jp/api/v1/csrf_token")
	t := csrf.Data
	h := func(r *resty.Request) *resty.Request { return r.SetHeader("x-csrf-token", t).SetHeader("Referer", "https://id.classi.jp/").SetHeader("Content-Type", "application/json") }
	h(c.resty.R()).SetBody(map[string]string{"username": uid}).Post("https://id-api.classi.jp/api/v1/login_methods")
	h(c.resty.R()).SetBody(map[string]interface{}{"username": uid, "password": pw, "saveId": false}).Post("https://id-api.classi.jp/api/v1/login/with_password")
	c.resty.R().Get("https://id-api.classi.jp/api/v1/login/continue")
	c.resty.R().SetResult(&csrf).Get("https://id-api.classi.jp/api/v1/csrf_token")
	t = csrf.Data
	r, err := c.resty.R().SetHeader("x-csrf-token", t).SetHeader("Referer", "https://id.classi.jp/").SetHeader("Content-Type", "application/json").SetBody(map[string]string{}).Post("https://id-api.classi.jp/api/v1/login/issue_cookie")
	if err != nil || r.StatusCode() != 200 { return fmt.Errorf("issue_cookie: %d", r.StatusCode()) }
	c.loggedIn = true
	return nil
}

func (c *ClassiClient) ensure() error { if c.loggedIn { return nil }; return c.Login("", "") }

type RawMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RawErr         `json:"error,omitempty"`
}

type RawErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var cli = NewClient()

func tools() []map[string]interface{} {
	t := []map[string]interface{}{
		{"name": "classi_login", "description": "Login to Classi", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"login_id": map[string]string{"type": "string"}, "password": map[string]string{"type": "string"}}}},
		{"name": "classi_groups", "description": "List all groups", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "classi_new_messages", "description": "Latest unread messages across all groups", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "classi_group_messages", "description": "Messages in a specific group", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"group_id": map[string]string{"type": "integer"}, "page": map[string]string{"type": "integer"}}}},
		{"name": "classi_read_message", "description": "Read a specific message with full details including attachments", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"message_id": map[string]string{"type": "integer"}}, "required": []string{"message_id"}}},
		{"name": "classi_download_file", "description": "Download an attachment file (PDF, image, etc.) from a Classi message. Returns the saved file path. Use entry_cd from message attachments.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"entry_cd": map[string]string{"type": "string", "description": "The entry_cd from the message attach array"}, "filename": map[string]string{"type": "string", "description": "Optional filename override"}}, "required": []string{"entry_cd"}}},
		{"name": "classi_notifications", "description": "Service notifications", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"limit": map[string]string{"type": "integer"}}}},
		{"name": "classi_calendar", "description": "School calendar events", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"start_date": map[string]string{"type": "string"}, "end_date": map[string]string{"type": "string"}}}},
		{"name": "classi_study_form", "description": "Daily study record form", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"date": map[string]string{"type": "string"}}}},
	}
	if enableLearningReport {
		t = append(t, map[string]interface{}{
			"name":        "classi_learning_report",
			"description": "Learning report (成績カルテ) — cumulative study minutes per day",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{
				"user_id": map[string]string{"type": "integer", "description": "Student ID (default: env CLASSI_ID)"},
			}},
		})
	}
	return t
}

func main() {
	os.MkdirAll(downloadDir, 0755)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var msg RawMsg
		if json.Unmarshal(sc.Bytes(), &msg) != nil { continue }

		switch msg.Method {
		case "initialize":
			send(RawMsg{JSONRPC: "2.0", ID: msg.ID, Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]bool{}},
				"serverInfo":      map[string]string{"name": "classi-mcp", "version": "1.4.0"},
			}})
		case "notifications/initialized":
		case "tools/list":
			send(RawMsg{JSONRPC: "2.0", ID: msg.ID, Result: map[string]interface{}{"tools": tools()}})
		case "tools/call":
			var p struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			json.Unmarshal(msg.Params, &p)

			if p.Name != "classi_login" {
				if err := cli.ensure(); err != nil {
					sendErr(msg.ID, -1, err.Error())
					continue
				}
			}

			var result interface{}
			switch p.Name {
			case "classi_login":
				uid, _ := p.Arguments["login_id"].(string)
				pw, _ := p.Arguments["password"].(string)
				if err := cli.Login(uid, pw); err != nil {
					result = err.Error()
				} else {
					result = "Logged in"
				}
			case "classi_groups":
				var d struct{ Groups []struct{ ID int; Name string; Unread int } }
				cli.resty.R().SetResult(&d).Get("https://platform.classi.jp/api/v2/groups/")
				var gs []map[string]interface{}
				for _, g := range d.Groups { gs = append(gs, map[string]interface{}{"name": g.Name, "id": g.ID, "unread": g.Unread}) }
				result = gs
			case "classi_new_messages":
				var d []struct {
					Group   struct{ Name string } `json:"group"`
					Message struct {
						ID   int `json:"id"`
						Body struct {
							Text   string `json:"text"`
							Attach []struct {
								EntryCD  string `json:"entry_cd"`
								Type     int    `json:"type"`
								FileName string `json:"file_name"`
								DownloadURL string `json:"download_url"`
							} `json:"attach"`
						} `json:"body"`
					} `json:"message"`
				}
				cli.resty.R().SetResult(&d).Get("https://platform.classi.jp/api/v2/groups/newmessages")
				var msgs []map[string]interface{}
				for _, m := range d {
					entry := map[string]interface{}{"group": m.Group.Name, "message_id": m.Message.ID, "text": trunc(m.Message.Body.Text, 200)}
					if len(m.Message.Body.Attach) > 0 {
						var atts []map[string]string
						for _, a := range m.Message.Body.Attach {
							atts = append(atts, map[string]string{"entry_cd": a.EntryCD, "file_name": a.FileName, "type": fmt.Sprintf("%d", a.Type)})
						}
						entry["attachments"] = atts
					}
					msgs = append(msgs, entry)
				}
				result = msgs
			case "classi_group_messages":
				gid := int(getFloat(p.Arguments, "group_id"))
				page := 1
				if v := getFloat(p.Arguments, "page"); v > 0 { page = int(v) }
				var data interface{}
				cli.resty.R().SetResult(&data).Get(fmt.Sprintf("https://platform.classi.jp/api/v2/groups/%d/messages?page=%d", gid, page))
				result = data
			case "classi_read_message":
				mid := int(getFloat(p.Arguments, "message_id"))
				var data interface{}
				cli.resty.R().SetResult(&data).Get(fmt.Sprintf("https://platform.classi.jp/api/v3/group_messages/%d", mid))
				result = data
			case "classi_download_file":
				entryCD, _ := p.Arguments["entry_cd"].(string)
				filename, _ := p.Arguments["filename"].(string)
				if entryCD == "" {
					result = "Error: entry_cd is required"
					break
				}
				// Download the file
				resp, err := cli.resty.R().SetDoNotParseResponse(true).Get(
					fmt.Sprintf("https://platform.classi.jp/api/cbank/%s/download", entryCD))
				if err != nil {
					result = fmt.Sprintf("Download failed: %v", err)
					break
				}
				defer resp.RawBody().Close()
				// Determine filename from Content-Disposition or use provided name
				if filename == "" {
					cd := resp.Header().Get("Content-Disposition")
					if strings.Contains(cd, "filename=") {
						parts := strings.Split(cd, "filename=")
						if len(parts) > 1 {
							filename = strings.Trim(parts[1], "\"; ")
						}
					}
					if filename == "" {
						filename = entryCD
					}
				}
				savePath := filepath.Join(downloadDir, filename)
				f, err := os.Create(savePath)
				if err != nil {
					result = fmt.Sprintf("Save failed: %v", err)
					break
				}
				written, _ := io.Copy(f, resp.RawBody())
				f.Close()
				result = map[string]interface{}{
					"path":      savePath,
					"size":      written,
					"filename":  filename,
					"entry_cd":  entryCD,
					"mime_type": resp.Header().Get("Content-Type"),
				}
			case "classi_notifications":
				limit := 10
				if v := getFloat(p.Arguments, "limit"); v > 0 { limit = int(v) }
				var d struct{ Items []struct{ Body, PublishDate string } }
				cli.resty.R().SetResult(&d).Get("https://platform.classi.jp/communication/api/v1/notification/service")
				var items []map[string]interface{}
				for _, item := range d.Items {
					if limit > 0 && len(items) >= limit { break }
					items = append(items, map[string]interface{}{"body": trunc(item.Body, 200), "date": item.PublishDate})
				}
				result = items
			case "classi_calendar":
				sd, _ := p.Arguments["start_date"].(string)
				ed, _ := p.Arguments["end_date"].(string)
				if sd == "" { sd = time.Now().AddDate(0, 0, -7).Format("2006-01-02") }
				if ed == "" { ed = time.Now().AddDate(0, 0, 7).Format("2006-01-02") }
				var data interface{}
				cli.resty.R().SetResult(&data).Get(fmt.Sprintf("https://platform.classi.jp/api/event/list?start_at=%s&end_at=%s", sd, ed))
				result = data
			case "classi_study_form":
				d, _ := p.Arguments["date"].(string)
				if d == "" { d = time.Now().Format("2006-01-02") }
				var data interface{}
				cli.resty.R().SetHeader("X-Requested-With", "XMLHttpRequest").SetResult(&data).Get("https://study.classi.jp/api/study/my_report/form?date=" + d)
				result = data
			case "classi_learning_report":
				uid := classiID
				if v := getFloat(p.Arguments, "user_id"); v > 0 { uid = fmt.Sprintf("%d", int(v)) }
				cli.resty.R().Get(fmt.Sprintf("https://karte.classi.jp/student/learnings_report/%s", uid))
				var data interface{}
				cli.resty.R().SetResult(&data).Get(fmt.Sprintf("https://karte.classi.jp/api/users/%s/subject_learning_reports", uid))
				result = data
			default:
				sendErr(msg.ID, -32601, "Unknown: "+p.Name)
				continue
			}
			text, _ := json.MarshalIndent(result, "", "  ")
			send(RawMsg{JSONRPC: "2.0", ID: msg.ID, Result: map[string]interface{}{"content": []map[string]string{{"type": "text", "text": string(text)}}}})
		default:
			sendErr(msg.ID, -32601, "Unknown: "+msg.Method)
		}
	}
}

func send(msg RawMsg) {
	out, _ := json.Marshal(msg)
	os.Stdout.Write(out)
	os.Stdout.Write([]byte("\n"))
}

func sendErr(id *int, code int, message string) {
	send(RawMsg{JSONRPC: "2.0", ID: id, Error: &RawErr{Code: code, Message: message}})
}

func getFloat(args map[string]interface{}, k string) float64 { v, _ := args[k].(float64); return v }
func trunc(s string, n int) string { r := []rune(s); if len(r) <= n { return s }; return string(r[:n]) + "..." }

// unused imports are needed for base64
var _ = base64.StdEncoding
