package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http/cookiejar"
	"os"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

// ─── Configuration (all from env) ───

var classiID = os.Getenv("CLASSI_ID")
var classiPW = os.Getenv("CLASSI_PASSWORD")

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" { return def }
	b, err := strconv.ParseBool(v)
	if err != nil { return def }
	return b
}

var (
	enablePost   = envBool("CLASSI_ENABLE_POST", false)
	enableStudy  = envBool("CLASSI_ENABLE_STUDY", true)
	enableCal    = envBool("CLASSI_ENABLE_CALENDAR", true)
)

// ─── Client ───

type ClassiClient struct {
	resty    *resty.Client
	loggedIn bool
}

func NewClient() *ClassiClient {
	jar, _ := cookiejar.New(nil)
	c := &ClassiClient{
		resty: resty.New().
			SetCookieJar(jar).
			SetHeader("User-Agent", "Mozilla/5.0").
			SetHeader("Accept", "application/json, text/plain, */*").
			SetTimeout(15 * time.Second),
	}
	return c
}

// ─── Login ───

func (c *ClassiClient) Login(uid, pw string) (bool, string) {
	if uid == "" { uid = classiID }
	if pw == "" { pw = classiPW }
	if uid == "" || pw == "" { return false, "Missing CLASSI_ID or CLASSI_PASSWORD env" }

	c.resty.R().Get("https://id.classi.jp/login/identifier")

	var csrf struct{ Data string }
	c.resty.R().SetResult(&csrf).Get("https://id-api.classi.jp/api/v1/csrf_token")
	token := csrf.Data

	h := func(r *resty.Request) *resty.Request {
		return r.SetHeader("x-csrf-token", token).
			SetHeader("Referer", "https://id.classi.jp/").
			SetHeader("Content-Type", "application/json")
	}

	h(c.resty.R()).SetBody(map[string]string{"username": uid}).Post("https://id-api.classi.jp/api/v1/login_methods")
	h(c.resty.R()).SetBody(map[string]interface{}{"username": uid, "password": pw, "saveId": false}).Post("https://id-api.classi.jp/api/v1/login/with_password")
	c.resty.R().Get("https://id-api.classi.jp/api/v1/login/continue")

	c.resty.R().SetResult(&csrf).Get("https://id-api.classi.jp/api/v1/csrf_token")
	token = csrf.Data
	r, err := h(c.resty.R()).SetBody(map[string]string{}).Post("https://id-api.classi.jp/api/v1/login/issue_cookie")
	if err != nil || r.StatusCode() != 200 { return false, "Login failed: issue_cookie" }

	var info struct{ User struct{ Name string } }
	r2, _ := c.resty.R().SetResult(&info).Get("https://platform.classi.jp/api/user/info")
	if r2.StatusCode() != 200 { return false, "Login failed: verify" }

	c.loggedIn = true
	return true, "Logged in as " + info.User.Name
}

// ─── Groups ───

func (c *ClassiClient) GetGroups() []map[string]interface{} {
	var data struct{ Groups []struct{ ID int; Name string; Unread int } }
	c.resty.R().SetResult(&data).Get("https://platform.classi.jp/api/v2/groups/")
	var gs []map[string]interface{}
	for _, g := range data.Groups { gs = append(gs, map[string]interface{}{"name":g.Name,"id":g.ID,"unread":g.Unread}) }
	return gs
}

// ─── Messages ───

func (c *ClassiClient) GetGroupMessages(groupID, page int) interface{} {
	var data interface{}
	c.resty.R().SetResult(&data).Get(fmt.Sprintf("https://platform.classi.jp/api/v2/groups/%d/messages?page=%d", groupID, page))
	return data
}

func (c *ClassiClient) GetNewMessages() []map[string]interface{} {
	var data []struct {
		Group struct{ Name string } `json:"group"`
		Message struct{ ID int; Body struct{ Text string } } `json:"message"`
	}
	c.resty.R().SetResult(&data).Get("https://platform.classi.jp/api/v2/groups/newmessages")
	var msgs []map[string]interface{}
	for _, m := range data { msgs = append(msgs, map[string]interface{}{"group":m.Group.Name,"message_id":m.Message.ID,"text":trunc(m.Message.Body.Text,200)}) }
	return msgs
}

func (c *ClassiClient) GetMessage(mid int) map[string]interface{} {
	var data struct{ Message struct{ Body struct{ Text, Subject string }; User struct{ Name string }; CreatedAt string; CommentCnt, LikeCnt, ReadCnt int } }
	c.resty.R().SetResult(&data).Get(fmt.Sprintf("https://platform.classi.jp/api/v3/group_messages/%d", mid))
	t := data.Message.Body.Text; if t == "" { t = data.Message.Body.Subject }
	return map[string]interface{}{"from":data.Message.User.Name,"text":t,"created":data.Message.CreatedAt,"comments":data.Message.CommentCnt,"likes":data.Message.LikeCnt,"read":data.Message.ReadCnt}
}

func (c *ClassiClient) PostMessage(groupID int, text string) map[string]string {
	if !enablePost { return map[string]string{"error":"Posting disabled. Set CLASSI_ENABLE_POST=true"} }
	resp, err := c.resty.R().SetHeader("Content-Type","application/json").SetBody(map[string]interface{}{"type":1,"body":map[string]string{"text":text}}).Post(fmt.Sprintf("https://platform.classi.jp/api/v2/groups/%d/messages", groupID))
	if err != nil { return map[string]string{"error":err.Error()} }
	if resp.StatusCode() != 200 && resp.StatusCode() != 201 { return map[string]string{"error":fmt.Sprintf("HTTP %d: %s",resp.StatusCode(),trunc(resp.String(),200))} }
	return map[string]string{"status":"posted","group_id":itoa(groupID)}
}

// ─── Notifications ───

func (c *ClassiClient) GetNotifications(limit int) []map[string]interface{} {
	var data struct{ Items []struct{ Body, PublishDate string } }
	c.resty.R().SetResult(&data).Get("https://platform.classi.jp/communication/api/v1/notification/service")
	var items []map[string]interface{}
	for i, item := range data.Items { if limit>0 && i>=limit { break }; items = append(items, map[string]interface{}{"body":trunc(item.Body,200),"date":item.PublishDate}) }
	return items
}

func (c *ClassiClient) GetUnreadCount() int {
	var data struct{ Count int }
	c.resty.R().SetResult(&data).Get("https://platform.classi.jp/communication/api/v1/notification/service/unreadcount")
	return data.Count
}

// ─── Study Records ───

var subjectNames = map[int]string{34:"国語",36:"社会",38:"数学",40:"理科",42:"英語",43:"その他",57:"読書"}

func (c *ClassiClient) GetStudyForm(date string) interface{} {
	if !enableStudy { return map[string]string{"error":"Study disabled. Set CLASSI_ENABLE_STUDY=true"} }
	if date == "" { date = time.Now().Format("2006-01-02") }
	var data interface{}
	resp, _ := c.resty.R().SetHeader("X-Requested-With","XMLHttpRequest").SetResult(&data).Get("https://study.classi.jp/api/study/my_report/form?date="+date)
	if resp != nil && resp.StatusCode()==200 { return data }
	return map[string]string{"error":fmt.Sprintf("HTTP %d",resp.StatusCode())}
}

func (c *ClassiClient) getStudyCSRF() string {
	resp, _ := c.resty.R().Get("https://study.classi.jp/")
	for _, line := range splitLines(resp.String()) {
		if i := indexOf(line,`name="csrf-token"`); i>=0 {
			if s := indexAfter(line,`content="`,i); s>=0 {
				if e := indexFrom(line,`"`,s); e>s { return line[s:e] }
			}
		}
	}
	return ""
}

func (c *ClassiClient) SaveStudyRecord(jsonStr string) interface{} {
	if !enableStudy { return map[string]string{"error":"Study disabled"} }
	csrf := c.getStudyCSRF(); if csrf == "" { return map[string]string{"error":"CSRF token not found"} }
	var body interface{}
	if err := json.Unmarshal([]byte(jsonStr), &body); err != nil { return map[string]string{"error":"Invalid JSON: "+err.Error()} }
	resp, _ := c.resty.R().SetHeader("Content-Type","application/json").SetHeader("X-Requested-With","XMLHttpRequest").SetHeader("X-CSRF-Token",csrf).SetBody(body).Put("https://study.classi.jp/api/study/my_report/form")
	if resp != nil && resp.StatusCode()==200 { return map[string]string{"status":"saved"} }
	return map[string]string{"error":fmt.Sprintf("HTTP %d",resp.StatusCode())}
}

func (c *ClassiClient) QuickStudyRecord(date string, subjects map[string]int, times map[string]string, msg string) interface{} {
	if date == "" { date = time.Now().Format("2006-01-02") }
	var reports []map[string]interface{}
	for name, minutes := range subjects {
		id := 43
		for k, v := range subjectNames { if v == name { id = k; break } }
		reports = append(reports, map[string]interface{}{"subjectId":id,"learnedMinutes":minutes})
	}
	body := map[string]interface{}{
		"date":date,
		"activityTimeReports":map[string]interface{}{"awokeAt":getStr(times,"awoke"),"schoolAt":getStr(times,"school"),"homeAt":getStr(times,"home"),"workStartedAt":getStr(times,"work"),"sleptAt":getStr(times,"slept")},
		"satisfactionRate":map[string]int{"timeRating":3,"contentRating":3},
		"subjectLearningReports":reports,
		"studentMessage":msg,
	}
	b, _ := json.Marshal(body)
	return c.SaveStudyRecord(string(b))
}

// ─── Calendar ───

func (c *ClassiClient) GetCalendarEvents(start, end string) interface{} {
	if !enableCal { return map[string]string{"error":"Calendar disabled. Set CLASSI_ENABLE_CALENDAR=true"} }
	if start == "" { start = time.Now().AddDate(0,0,-7).Format("2006-01-02") }
	if end == "" { end = time.Now().AddDate(0,0,7).Format("2006-01-02") }
	var data interface{}
	c.resty.R().SetResult(&data).Get(fmt.Sprintf("https://platform.classi.jp/api/event/list?start_at=%s&end_at=%s", start, end))
	return data
}

// ─── Helpers ───

func getStr(m map[string]string, k string) string { if v,ok:=m[k];ok{return v};return "" }
func itoa(n int) string { return fmt.Sprintf("%d",n) }
func trunc(s string, n int) string { r:=[]rune(s);if len(r)<=n{return s};return string(r[:n])+"..." }
func splitLines(s string) []string { var l []string;st:=0;for i:=0;i<len(s);i++{if s[i]=='\n'{l=append(l,s[st:i]);st=i+1}};if st<len(s){l=append(l,s[st:])};return l }
func indexOf(s,sub string) int { for i:=0;i<=len(s)-len(sub);i++{if s[i:i+len(sub)]==sub{return i}};return -1 }
func indexAfter(s,sub string,start int) int { for i:=start;i<=len(s)-len(sub);i++{if s[i:i+len(sub)]==sub{return i+len(sub)}};return -1 }
func indexFrom(s,sub string,start int) int { for i:=start;i<=len(s)-len(sub);i++{if s[i:i+len(sub)]==sub{return i}};return -1 }

// ─── MCP Protocol ───

type rpcError = struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

var cli = NewClient()

func tool(name, desc string, props map[string]interface{}, required ...string) map[string]interface{} {
	t := map[string]interface{}{"name":name,"description":desc,"inputSchema":map[string]interface{}{"type":"object","properties":props}}
	if len(required) > 0 { t["inputSchema"].(map[string]interface{})["required"] = required }
	return t
}
func sp(d string) map[string]string { return map[string]string{"type":"string","description":d} }
func ip(d string) map[string]string { return map[string]string{"type":"integer","description":d} }

func respond(id int, data interface{}) *rpcMsg {
	text, _ := json.MarshalIndent(data, "", "  ")
	return &rpcMsg{JSONRPC:"2.0",ID:id,Result:map[string]interface{}{"content":[]map[string]string{{"type":"text","text":string(text)}}}}
}

func errMsg(id int, code int, msg string) *rpcMsg {
	return &rpcMsg{JSONRPC:"2.0",ID:id,Error:&rpcError{code,msg}}
}

func handle(msg rpcMsg) *rpcMsg {
	switch msg.Method {
	case "initialize":
		return &rpcMsg{JSONRPC:"2.0",ID:msg.ID,Result:map[string]interface{}{
			"protocolVersion":"2024-11-05","capabilities":map[string]bool{},
			"serverInfo":map[string]string{"name":"classi-mcp","version":"1.1.0"},
		}}
	case "tools/list":
		tools := []map[string]interface{}{
			tool("classi_login","Login to Classi (auto-login if env vars set)",map[string]interface{}{"login_id":sp("Classi ID"),"password":sp("Password")}),
			tool("classi_groups","List school groups with unread counts",map[string]interface{}{}),
			tool("classi_new_messages","Get latest messages across all groups",map[string]interface{}{}),
			tool("classi_group_messages","Get messages from a specific group",map[string]interface{}{"group_id":ip("Group ID"),"page":ip("Page (default 1)")}),
			tool("classi_read_message","Read a message with full details",map[string]interface{}{"message_id":ip("Message ID")},"message_id"),
			tool("classi_notifications","Get notifications with unread count",map[string]interface{}{"limit":ip("Max items (default 10)")}),
		}
		if enablePost { tools = append(tools, tool("classi_post_message","Post a message to a group",map[string]interface{}{"group_id":ip("Group ID"),"text":sp("Message text")},"group_id","text")) }
		if enableStudy {
			tools = append(tools,
				tool("classi_study_form","Get study record form data",map[string]interface{}{"date":sp("YYYY-MM-DD (default: today)")}),
				tool("classi_study_quick","Quick save study record",map[string]interface{}{"date":sp("YYYY-MM-DD"),"subjects":map[string]interface{}{"type":"object","description":"{\"数学\":60,\"英語\":30}"},"times":map[string]interface{}{"type":"object","description":"{\"awoke\":\"07:00\",...}"},"message":sp("Comment")}),
				tool("classi_study_save","Save study record from full JSON",map[string]interface{}{"json":sp("Full study record JSON")},"json"),
			)
		}
		if enableCal { tools = append(tools, tool("classi_calendar","Get calendar events for a date range",map[string]interface{}{"start_date":sp("YYYY-MM-DD"),"end_date":sp("YYYY-MM-DD")})) }
		return &rpcMsg{JSONRPC:"2.0",ID:msg.ID,Result:map[string]interface{}{"tools":tools}}
	case "tools/call":
		var p struct{Name string;Arguments map[string]interface{}}
		json.Unmarshal(msg.Params,&p)
		if !cli.loggedIn {
			if ok,_:=cli.Login("",""); !ok { return errMsg(msg.ID,-1,"Login failed") }
		}
		var r interface{}
		switch p.Name {
		case "classi_login": u,_:=p.Arguments["login_id"].(string);pw,_:=p.Arguments["password"].(string);_,msg:=cli.Login(u,pw);r=msg
		case "classi_groups": r=cli.GetGroups()
		case "classi_new_messages": r=cli.GetNewMessages()
		case "classi_group_messages": g:=0;if v,ok:=p.Arguments["group_id"].(float64);ok{g=int(v)};pg:=1;if v,ok:=p.Arguments["page"].(float64);ok{pg=int(v)};r=cli.GetGroupMessages(g,pg)
		case "classi_read_message": if mid,ok:=p.Arguments["message_id"].(float64);ok{r=cli.GetMessage(int(mid))}
		case "classi_post_message": g:=0;if v,ok:=p.Arguments["group_id"].(float64);ok{g=int(v)};t,_:=p.Arguments["text"].(string);r=cli.PostMessage(g,t)
		case "classi_notifications": l:=10;if v,ok:=p.Arguments["limit"].(float64);ok{l=int(v)};items:=cli.GetNotifications(l);unread:=cli.GetUnreadCount();r=append([]map[string]interface{}{{"unread_total":unread}},items...)
		case "classi_calendar": s,_:=p.Arguments["start_date"].(string);e,_:=p.Arguments["end_date"].(string);r=cli.GetCalendarEvents(s,e)
		case "classi_study_form": d,_:=p.Arguments["date"].(string);r=cli.GetStudyForm(d)
		case "classi_study_quick":
			d,_:=p.Arguments["date"].(string);var subj map[string]int;var times map[string]string
			if raw,err:=json.Marshal(p.Arguments["subjects"]);err==nil{json.Unmarshal(raw,&subj)}
			if raw,err:=json.Marshal(p.Arguments["times"]);err==nil{json.Unmarshal(raw,&times)}
			msg,_:=p.Arguments["message"].(string);r=cli.QuickStudyRecord(d,subj,times,msg)
		case "classi_study_save": j,_:=p.Arguments["json"].(string);r=cli.SaveStudyRecord(j)
		default: return errMsg(msg.ID,-32601,"Unknown tool: "+p.Name)
		}
		return respond(msg.ID,r)
	case "notifications/initialized": return nil
	default: return errMsg(msg.ID,-32601,"Unknown: "+msg.Method)
	}
	return nil
}

func main() {
	sc := bufio.NewScanner(os.Stdin); sc.Buffer(make([]byte,1<<20),1<<20)
	for sc.Scan() {
		var msg rpcMsg
		if json.Unmarshal(sc.Bytes(),&msg)!=nil{continue}
		if resp:=handle(msg);resp!=nil{out,_:=json.Marshal(resp);fmt.Println(string(out))}
	}
}
