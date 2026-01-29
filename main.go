package main

import (
	"crypto/tls"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"gopkg.in/yaml.v3"
)

// 请求参数结构体
type RequestParams struct {
	Title      string `json:"title" form:"title"`
	Content    string `json:"content" form:"content"`
	AppID      string `json:"appid" form:"appid"`
	Secret     string `json:"secret" form:"secret"`
	UserID     string `json:"userid" form:"userid"`
	TemplateID string `json:"template_id" form:"template_id"`
	BaseURL    string `json:"base_url" form:"base_url"`
	Timezone   string `json:"tz" form:"tz"`
}

// 全局变量用于存储命令配置
var (
	appConfig *Config
)

// 微信AccessToken响应
type AccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// 微信模板消息请求
type TemplateMessageRequest struct {
	ToUser     string                 `json:"touser"`
	TemplateID string                 `json:"template_id"`
	URL        string                 `json:"url"`
	Data       map[string]interface{} `json:"data"`
}

// 微信API响应
type WechatAPIResponse struct {
	Errcode int    `json:"errcode"`
	Errmsg  string `json:"errmsg"`
}

func main() {
	// 加载配置文件
	cfg, err := LoadConfig("config.yml")
	if err != nil {
		fmt.Printf("Warning: Failed to load config.yml: %v. Using defaults.\n", err)
	}
	appConfig = cfg

	// 定义命令行参数变量
	var (
		cliTitle      string
		cliContent    string
		cliAppID      string
		cliSecret     string
		cliUserID     string
		cliTemplateID string
		cliBaseURL    string
		cliTimezone   string
		cliPort       string
	)

	// 定义命令行参数
	flag.StringVar(&cliTitle, "title", "", "消息标题")
	flag.StringVar(&cliContent, "content", "", "消息内容")
	flag.StringVar(&cliAppID, "appid", "", "AppID")
	flag.StringVar(&cliSecret, "secret", "", "AppSecret")
	flag.StringVar(&cliUserID, "userid", "", "openid")
	flag.StringVar(&cliTemplateID, "template_id", "", "模板ID")
	flag.StringVar(&cliBaseURL, "base_url", "", "跳转url")
	flag.StringVar(&cliTimezone, "tz", "", "时区，默认东八区")
	flag.StringVar(&cliPort, "port", "", "端口")

	// 解析命令行参数
	flag.Parse()

	// 命令行参数覆盖配置文件
	if cliTitle != "" {
		appConfig.Title = cliTitle
	}
	if cliContent != "" {
		appConfig.Content = cliContent
	}
	if cliAppID != "" {
		appConfig.AppID = cliAppID
	}
	if cliSecret != "" {
		appConfig.Secret = cliSecret
	}
	if cliUserID != "" {
		appConfig.UserID = cliUserID
	}
	if cliTemplateID != "" {
		appConfig.TemplateID = cliTemplateID
	}
	if cliBaseURL != "" {
		appConfig.BaseURL = cliBaseURL
	}
	if cliTimezone != "" {
		appConfig.Timezone = cliTimezone
	}
	if cliPort != "" {
		appConfig.Port = cliPort
	}

	// 设置路由
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/config", handleGetConfig)
	http.HandleFunc("/config/save", handleConfigSave)
	http.HandleFunc("/wxsend", handleWxSend)
	http.HandleFunc("/detail", handleDetail)

	// 启动服务器
	fmt.Println("Server is running on： " + "http://127.0.0.1:" + appConfig.Port)

	err = http.ListenAndServe(":"+appConfig.Port, nil)

	if err != nil {
		fmt.Printf("Error starting server: %v\n", err)
	}

}

// 嵌入静态HTML文件
//
//go:embed msg_detail.html index.html
var htmlContent embed.FS

// 处理首页（配置页）
func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	htmlData, err := htmlContent.ReadFile("index.html")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "Failed to read embedded HTML file: %v"}`, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(htmlData)
}

// 获取配置
func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(appConfig)
}

// 保存配置
func handleConfigSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var newConfig Config
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Update global config (partial update or full? Here we assume full from UI)
	// Ideally we should lock if concurrent access is an issue, but for simple app it's fine.
	*appConfig = newConfig

	// Marshaling to YAML and saving to file
	data, err := yaml.Marshal(&newConfig)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile("config.yml", data, 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Config saved"))
}

// 页面数据结构
type PageData struct {
	Title  string
	Result template.HTML // 预渲染的HTML内容
	Date   string
}

// 处理详情页面请求
func handleDetail(w http.ResponseWriter, r *http.Request) {
	// 解析 Query 参数
	title := r.URL.Query().Get("title")
	message := r.URL.Query().Get("message")
	date := r.URL.Query().Get("date")

	if title == "" {
		title = "消息推送"
	}
	if message == "" {
		message = "无告警信息"
	}
	if date == "" {
		date = "无时间信息"
	}

	// 使用 Goldmark 渲染 Markdown
	var buf strings.Builder
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	if err := md.Convert([]byte(message), &buf); err != nil {
		fmt.Printf("Markdown conversion failed: %v\n", err)
		buf.WriteString(message) // Fallback to raw text
	}

	data := PageData{
		Title:  title,
		Result: template.HTML(buf.String()),
		Date:   date,
	}

	// 解析模板
	tmpl, err := template.ParseFS(htmlContent, "msg_detail.html")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "Failed to parse template: %v"}`, err)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 执行模板
	if err := tmpl.Execute(w, data); err != nil {
		fmt.Printf("Template execution failed: %v\n", err)
	}
}

func handleWxSend(w http.ResponseWriter, r *http.Request) {

	// 解析参数
	var params RequestParams

	// 根据请求方法解析参数
	if r.Method == "POST" {
		// 解析JSON请求体
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&params)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error": "Invalid JSON format: %v"}`, err)
			return
		}
	} else if r.Method == "GET" {
		// 解析GET查询参数
		params.Title = r.URL.Query().Get("title")
		params.Content = r.URL.Query().Get("content")
		params.AppID = r.URL.Query().Get("appid")
		params.Secret = r.URL.Query().Get("secret")
		params.UserID = r.URL.Query().Get("userid")
		params.TemplateID = r.URL.Query().Get("template_id")
		params.BaseURL = r.URL.Query().Get("base_url")
		params.Timezone = r.URL.Query().Get("tz")
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprintf(w, `{"error": "Method not allowed"}`)
		return
	}

	// 使用全局配置填充缺失的参数
	if params.Title == "" {
		params.Title = appConfig.Title
	}
	if params.Content == "" {
		params.Content = appConfig.Content
	}
	if params.AppID == "" {
		params.AppID = appConfig.AppID
	}
	if params.Secret == "" {
		params.Secret = appConfig.Secret
	}
	if params.UserID == "" {
		params.UserID = appConfig.UserID
	}
	if params.TemplateID == "" {
		params.TemplateID = appConfig.TemplateID
	}
	if params.BaseURL == "" {
		params.BaseURL = appConfig.BaseURL
	}
	if params.Timezone == "" {
		params.Timezone = appConfig.Timezone
	}

	// 验证必要参数
	if params.AppID == "" || params.Secret == "" || params.UserID == "" || params.TemplateID == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "Missing required parameters"}`)
		return
	}
	if params.BaseURL == "" {
		// 尝试获取本机IP
		localIP, err := getLocalIP()
		port := appConfig.Port
		if port == "" {
			port = "5566"
		}
		if err == nil {
			params.BaseURL = "http://" + localIP + ":" + port
		} else {
			params.BaseURL = "https://push.hzz.cool"
		}
	}
	if params.Content == "" {
		params.Content = "测试内容"
	}
	if params.Title == "" {
		params.Title = "测试标题"
	}

	// 获取AccessToken
	token, err := getAccessToken(params.AppID, params.Secret)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "Failed to get access token: %v"}`, err)
		return
	}

	//log.Println(token)
	// 发送模板消息
	resp, err := sendTemplateMessage(token, params)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "Failed to send template message: %v"}`, err)
		return
	}

	// 返回结果
	json.NewEncoder(w).Encode(resp)
}

// Token请求参数结构体
type TokenRequestParams struct {
	GrantType    string `json:"grant_type"`
	AppID        string `json:"appid"`
	Secret       string `json:"secret"`
	ForceRefresh bool   `json:"force_refresh"`
}

func getAccessToken(appid, secret string) (string, error) {
	// 构建请求参数
	requestParams := TokenRequestParams{
		GrantType:    "client_credential",
		AppID:        appid,
		Secret:       secret,
		ForceRefresh: false,
	}

	// 转换为JSON
	jsonData, err := json.Marshal(requestParams)
	if err != nil {
		return "", err
	}

	// 忽略证书验证
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// 发送POST请求
	resp, err := client.Post("https://api.weixin.qq.com/cgi-bin/stable_token", "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	//log.Println(string(body))

	// 解析响应
	var tokenResp AccessTokenResponse
	err = json.Unmarshal(body, &tokenResp)
	//log.Println(tokenResp)

	if err != nil {
		return "", err
	}

	return tokenResp.AccessToken, nil
}

func sendTemplateMessage(accessToken string, params RequestParams) (WechatAPIResponse, error) {
	// 构建请求URL
	apiUrl := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/message/template/send?access_token=%s", accessToken)

	// 处理时区，默认东八区
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location, _ = time.LoadLocation("Asia/Shanghai") // 确保默认使用东八区
	}

	// 如果参数中有时区，则尝试使用该时区
	if params.Timezone != "" {
		loc, err := time.LoadLocation(params.Timezone)
		if err == nil {
			location = loc
		}
	}

	// 获取当前时间
	currentTime := time.Now().In(location)
	timeStr := currentTime.Format("2006-01-02 15:04:05")

	// 构建请求数据
	requestData := TemplateMessageRequest{
		ToUser:     params.UserID,
		TemplateID: params.TemplateID,
		URL:        params.BaseURL + `/detail?title=` + url.QueryEscape(params.Title) + `&message=` + url.QueryEscape(params.Content) + `&date=` + url.QueryEscape(timeStr),
		Data: map[string]interface{}{
			"title": map[string]string{
				"value": params.Title,
			},
			"content": map[string]string{
				"value": params.Content,
			},
		},
	}

	// 转换为JSON
	jsonData, err := json.Marshal(requestData)
	if err != nil {
		return WechatAPIResponse{}, err
	}

	// 忽略证书验证
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// 发送POST请求
	resp, err := client.Post(apiUrl, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return WechatAPIResponse{}, err
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return WechatAPIResponse{}, err
	}

	// 解析响应
	var apiResp WechatAPIResponse
	err = json.Unmarshal(body, &apiResp)
	if err != nil {
		return WechatAPIResponse{}, err
	}

	return apiResp, nil
}

// 获取本机内网IP
func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, address := range addrs {
		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}
	return "", fmt.Errorf("local IP not found")
}
