package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var authParamsCache = NewSafeCache(24 * time.Hour)
var client = resty.New().SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})

// Emby的API响应结构
type EmbyResponse struct {
	Items []struct {
		Name     string `json:"Name"`
		Id       string `json:"Id"`
		Type     string `json:"Type"`
		ServerId string `json:"ServerId"`
	} `json:"Items"`
}

// VOD的分类信息结构
type VodClass struct {
	TypeID   string `json:"type_id"`
	TypeName string `json:"type_name"`
}

// Emby API Video Item
type EmbyVideoItem struct {
	Name                    string            `json:"Name"`
	SeriesName              string            `json:"SeriesName"`
	SeasonName              string            `json:"SeasonName"`
	Id                      string            `json:"Id"`
	Overview                string            `json:"Overview"`
	RunTimeTicks            int64             `json:"RunTimeTicks"`
	ProductionYear          int               `json:"ProductionYear"`
	IsFolder                bool              `json:"IsFolder"`
	Type                    string            `json:"Type"`
	PrimaryImageAspectRatio float64           `json:"PrimaryImageAspectRatio"`
	ImageTags               map[string]string `json:"ImageTags"`
	MediaType               string            `json:"MediaType"`
	Path                    string            `json:"Path"`
	DateLastContentAdded    string            `json:"DateLastContentAdded"`
	UserData                struct {
		PlayCount         int `json:"PlayCount"`
		UnplayedItemCount int `json:"UnplayedItemCount"`
	} `json:"UserData"`
}

type EmbyCountItem struct {
	TotalRecordCount int             `json:"TotalRecordCount"`
	Items            []EmbyVideoItem `json:"Items"`
}

type EmbyAuthResponse struct {
	AccessToken string `json:"AccessToken"`
}

// VOD API Video List
type VodVideoItem struct {
	VodID       string `json:"vod_id"`
	TypeID      string `json:"type_id"`
	VodName     string `json:"vod_name"`
	VodSub      string `json:"vod_sub"`
	VodPic      string `json:"vod_pic"`
	VodOverview string `json:"vod_overview"`
	VodYear     int    `json:"vod_year"`
	VodArea     string `json:"vod_area"`
	VodTime     string `json:"vod_time"`
	VodDuration int    `json:"vod_duration"` // in minutes
	VodPlayFrom string `json:"vod_play_from,omitempty"`
	VodPlayUrl  string `json:"vod_play_url,omitempty"`
	VodTotal    int    `json:"vod_total"`
	VodContent  string `json:"vod_content,omitempty"`
	VodBlurb    string `json:"vod_blurb,omitempty"`
	VodRemarks  string `json:"vod_remarks,omitempty"`
}

type VodResponse struct {
	Code      int            `json:"code"`
	Msg       string         `json:"msg"`
	Page      string         `json:"page,omitempty"`
	PageCount int            `json:"pagecount,omitempty"`
	Limit     string         `json:"limit,omitempty"`
	Total     int            `json:"total,omitempty"`
	List      []VodVideoItem `json:"list,omitempty"`
	Class     []VodClass     `json:"class,omitempty"`
}

// Fetch and convert video list from Emby to VOD format
func fetchAndConvertVideoList(c *gin.Context, parentId string, page int, search string) (*VodResponse, error) {
	resp, err := client.R().
		SetHeader("Accept", "application/json").
		Get(upstreamUrl(c) + "/emby/Users/e166b644a9e642d4ad2a1d71c38a1c76/Items?IncludeItemTypes=Series&Recursive=true&fields=" + url.QueryEscape("Overview,ProductionYear,DateLastContentAdded") + "&SortBy=ProductionYear%2CPremiereDate%2CSortName&SortOrder=Descending&Limit=20&SearchTerm=" + url.QueryEscape(search) + "&StartIndex=" + strconv.Itoa(page*20) + "&ParentId=" + parentId + "&" + upstreamAuthParams(c))
	if err != nil {
		return nil, err
	}

	var result EmbyCountItem
	err = json.Unmarshal(resp.Body(), &result)
	if err != nil {
		return nil, err
	}

	vodItems := make([]VodVideoItem, len(result.Items))
	for i, item := range result.Items {
		vodItems[i] = VodVideoItem{
			VodID:       item.Id,
			TypeID:      parentId,
			VodName:     item.Name,
			VodSub:      item.Overview,
			VodPic:      upstreamUrl(c) + "/emby/Items/" + item.Id + "/Images/Primary?maxHeight=375&maxWidth=250&tag=" + item.ImageTags["Primary"] + "&quality=90",
			VodOverview: item.Overview,
			VodBlurb:    item.Overview,
			VodYear:     item.ProductionYear,
			VodTime:     item.DateLastContentAdded,
			VodTotal:    item.UserData.PlayCount + item.UserData.UnplayedItemCount,
			VodRemarks:  "更新至：" + strconv.Itoa(item.UserData.PlayCount+item.UserData.UnplayedItemCount),
		}
	}

	return &VodResponse{Code: 1, Msg: "数据列表", Page: strconv.Itoa(page), Total: result.TotalRecordCount, Limit: strconv.Itoa(20), PageCount: result.TotalRecordCount/20 + 1, List: vodItems}, nil
}

func fetchAndConvertDetail(c *gin.Context, id string) ([]VodVideoItem, error) {
	resp, err := client.R().
		SetHeader("Accept", "application/json").
		Get(upstreamUrl(c) + "/emby/Users/e166b644a9e642d4ad2a1d71c38a1c76/Items/" + id + "?fields=Overview&" + upstreamAuthParams(c))
	if err != nil {
		return nil, err
	}

	var result EmbyVideoItem
	err = json.Unmarshal(resp.Body(), &result)
	if err != nil {
		return nil, err
	}

	vodItems := make([]VodVideoItem, 1)
	vodItems[0] = EmbyItemToPlayableVideoItem(c, result)

	if !result.IsFolder {
		return vodItems, nil
	}

	resp, err = client.R().
		SetHeader("Accept", "application/json").
		Get(upstreamUrl(c) + "/emby/Users/e166b644a9e642d4ad2a1d71c38a1c76/Items?IncludeItemTypes=Episode&Recursive=true&ParentId=" + id + "&" + upstreamAuthParams(c))
	if err != nil {
		return nil, err
	}

	log.Println(resp.String())
	var seasonResult EmbyCountItem

	err = json.Unmarshal(resp.Body(), &seasonResult)
	if err != nil {
		return nil, err
	}

	episodeUrls := make([]string, len(seasonResult.Items))
	for i, item := range seasonResult.Items {
		episodeUrls[i] = item.SeasonName + item.Name + "$" + upstreamUrl(c) + "/emby/videos/" + item.Id + "/original.mp4?" + upstreamAuthParams(c)
	}
	log.Println(strings.Join(episodeUrls, "#"))
	vodItems[0].VodPlayUrl = strings.Join(episodeUrls, "#")

	return vodItems, nil
}

func EmbyItemToPlayableVideoItem(c *gin.Context, item EmbyVideoItem) VodVideoItem {
	return VodVideoItem{
		VodID:       item.Id,
		VodName:     item.Name,
		VodSub:      item.Overview,
		VodPic:      upstreamUrl(c) + "/emby/Items/" + item.Id + "/Images/Primary?maxHeight=375&maxWidth=250&tag=" + item.ImageTags["Primary"] + "&quality=90",
		VodOverview: item.Overview,
		VodContent:  item.Overview,
		VodYear:     item.ProductionYear,
		VodDuration: int(item.RunTimeTicks / 600000000), // Converting ticks to minutes
		VodTotal:    item.UserData.PlayCount + item.UserData.UnplayedItemCount,
		VodPlayFrom: "emby",
		VodPlayUrl:  upstreamUrl(c) + "/emby/videos/" + item.Id + "/original.mp4?" + upstreamAuthParams(c),
	}
}

func upstreamAuthParams(c *gin.Context) string {
	accountBase64 := c.Param("account")
	return authParamsCache.ComputeIfAbsent(accountBase64, func() interface{} {
		return upstreamAuthParamsInternal(c, accountBase64)
	}, 24*time.Hour).(string)
}

func upstreamAuthParamsInternal(c *gin.Context, accountBase64 string) string {
	accountUrl, err := base64.StdEncoding.DecodeString(accountBase64)
	if err != nil {
		log.Panic(err)
	}
	account, err := url.Parse(string(accountUrl))
	if err != nil {
		log.Panic(err)
	}
	commonParams := upstreamCommonParams(c)
	password, _ := account.User.Password()

	resp, err := client.R().
		SetHeader("Accept", "application/json").
		SetFormData(map[string]string{"Username": account.User.Username(), "Pw": password}).
		Post(upstreamUrl(c) + "/emby/Users/authenticatebyname?" + commonParams)
	if err != nil {
		log.Panic(err)
	}

	if resp.StatusCode() != 200 {
		log.Panic("登录 Emby 失败：" + resp.Status() + " " + resp.String())
	}

	var authResponse EmbyAuthResponse
	err = json.Unmarshal(resp.Body(), &authResponse)
	if err != nil {
		log.Panic(err)
	}

	return commonParams + "&X-Emby-Token=" + authResponse.AccessToken
}

func upstreamCommonParams(c *gin.Context) string {
	hostname, err := os.Hostname()
	if err != nil {
		log.Panic(err)
	}
	return "X-Emby-Client=Vod+Converter&X-Emby-Device-Name=" + url.QueryEscape(hostname+"-XA") + "&X-Emby-Device-Id=" + uuid.New().String() + "&X-Emby-Client-Version=1.0.0&X-Emby-Language=zh-cn"
}

func upstreamUrl(c *gin.Context) string {
	accountBase64 := c.Param("account")
	accountUrl, err := base64.StdEncoding.DecodeString(accountBase64)
	if err != nil {
		log.Panic(err)
	}
	account, err := url.Parse(string(accountUrl))
	if err != nil {
		log.Panic(err)
	}
	return account.Scheme + "://" + account.Host + ":" + account.Port()
}

// 处理响应的函数，转换Emby格式到VOD格式
func fetchAndConvertClass(c *gin.Context) ([]VodClass, error) {
	resp, err := client.R().
		SetHeader("Accept", "application/json").
		Get(upstreamUrl(c) + "/emby/Users/e166b644a9e642d4ad2a1d71c38a1c76/Views" + "?" + upstreamAuthParams(c))
	if err != nil {
		return nil, err
	}

	var embyResponse EmbyResponse

	err = json.Unmarshal(resp.Body(), &embyResponse)
	if err != nil {
		return nil, err
	}

	vodClasses := make([]VodClass, len(embyResponse.Items))
	for i, item := range embyResponse.Items {
		vodClasses[i] = VodClass{
			TypeID:   item.Id,
			TypeName: item.Name,
		}
	}

	return vodClasses, nil
}

func main() {
	router := gin.Default()
	router.Use(gin.CustomRecovery(func(c *gin.Context, err interface{}) {
		log.Println(err)
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "数据列表", "list": []VodVideoItem{{VodName: err.(string)}}})
	}))
	router.GET("/:account", func(c *gin.Context) {
		ac, _ := c.GetQuery("ac")
		switch ac {
		case "class":
			vodClasses, err := fetchAndConvertClass(c)
			if err != nil {
				log.Println(err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process data"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "数据列表", "class": vodClasses})
			break
		case "detail":
			items, err := fetchAndConvertDetail(c, c.Query("ids"))
			if err != nil {
				log.Println(err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process data"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "数据列表", "list": items})
			break
		default:
			pg, _ := strconv.Atoi(c.DefaultQuery("pg", "1"))
			resp, err := fetchAndConvertVideoList(c, c.Query("t"), pg, c.DefaultQuery("wd", ""))
			if err != nil {
				log.Println(err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process data"})
				return
			}
			c.JSON(http.StatusOK, resp)
			break
		}
	})

	log.Fatal(router.Run(":8080"))
}
