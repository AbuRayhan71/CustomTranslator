package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"io/ioutil"
	"net/http"
	"strings"
)

type EventInfo struct {
	Name             string            `json:"name" validate:"required"`
	Location         string            `json:"location" validate:"required"`
	Details          string            `json:"details" validate:"required"`
	LinkNames        map[string]string `json:"linkNames" validate:"dive,keys,required,endkeys,required"`
	SponsoredMessage string            `json:"sponsoredMessage"`
	Languages        []string          `json:"languages" validate:"required,dive,required"`
	Keywords         []string          `json:"keywords" validate:"dive,required"`
	Translations     map[string]string `json:"translations"`
}

type TranslationRequest struct {
	Text string `json:"Text"`
}

type TranslationResponse struct {
	Translations []struct {
		Text string `json:"text"`
	} `json:"translations"`
}

var (
	events map[string]EventInfo

	validate *validator.Validate
)

func init() {
	events = make(map[string]EventInfo)
	validate = validator.New()
}

func translateText(text, targetLanguage, url, subscriptionKey, location string) (string, error) {
	body := []TranslationRequest{{Text: text}}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("error marshaling json: %v", err)
	}

	req, err := http.NewRequest("POST", url+"&to="+targetLanguage, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Add("Ocp-Apim-Subscription-Key", subscriptionKey)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Ocp-Apim-Subscription-Region", location)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error making translation request: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("non-OK HTTP status: %d, response: %s", resp.StatusCode, respBody)
	}

	var res []TranslationResponse
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", fmt.Errorf("error decoding response body: %v", err)
	}

	if len(res) > 0 && len(res[0].Translations) > 0 {
		return res[0].Translations[0].Text, nil
	}

	return "", fmt.Errorf("no translations found in the response")
}

func replaceKeywordsWithPlaceholders(text string, keywords []string) (string, map[string]string) {
	placeholderMap := make(map[string]string)
	for i, keyword := range keywords {
		placeholder := fmt.Sprintf("KW%dPLH", i)
		text = strings.ReplaceAll(text, keyword, placeholder)
		placeholderMap[placeholder] = keyword
	}
	return text, placeholderMap
}

func replacePlaceholdersWithKeywords(text string, placeholderMap map[string]string) string {
	for placeholder, keyword := range placeholderMap {
		text = strings.ReplaceAll(text, placeholder, keyword)
	}
	return text
}

func postEvent(c *gin.Context) {
	var event EventInfo
	if err := c.BindJSON(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, exists := events[event.Name]; exists {
		c.JSON(http.StatusConflict, gin.H{"message": "Event already exists"})
		return
	}

	var detailsBuilder strings.Builder
	detailsBuilder.WriteString(event.Name + " ")
	detailsBuilder.WriteString("Location: " + event.Location + " ")
	detailsBuilder.WriteString("Details: " + event.Details + " ")
	for _, link := range event.LinkNames {
		detailsBuilder.WriteString(link + " ")
	}
	detailsBuilder.WriteString(event.SponsoredMessage)

	preparedText, placeholderMap := replaceKeywordsWithPlaceholders(detailsBuilder.String(), event.Keywords)

	endpoint := "https://api.cognitive.microsofttranslator.com"
	uri := endpoint + "/translate?api-version=3.0"
	location := "eastus"
	key := ""

	event.Translations = make(map[string]string)
	for _, lang := range event.Languages {
		translatedText, err := translateText(preparedText, lang, uri, key, location)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error translating to %s: %v", lang, err)})
			return
		}

		finalText := replacePlaceholdersWithKeywords(translatedText, placeholderMap)
		event.Translations[lang] = finalText
	}

	events[event.Name] = event
	c.JSON(http.StatusCreated, event)
}

func getEvent(c *gin.Context) {
	eventType := c.Query("type")

	if event, ok := events[eventType]; ok {
		c.JSON(http.StatusOK, event)
	} else {
		c.JSON(http.StatusNotFound, gin.H{"error": "Event not found"})
	}
}

func main() {
	r := gin.Default()
	r.POST("/event", postEvent)
	r.GET("/event", getEvent)
	r.Run()
}
