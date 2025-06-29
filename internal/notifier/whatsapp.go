package notifier

import (
	"fmt"
	"net/http"
	"net/url"
)

type WhatsAppNotifier struct {
	Phone  string
	APIKey string
}

func NewWhatsAppNotifier(phone, apiKey string) *WhatsAppNotifier {
	return &WhatsAppNotifier{
		Phone:  phone,
		APIKey: apiKey,
	}
}

func (n *WhatsAppNotifier) Send(message string) {
	baseURL := "https://api.callmebot.com/whatsapp.php"

	params := url.Values{}
	params.Add("phone", n.Phone)
	params.Add("text", message)
	params.Add("apikey", n.APIKey)

	reqURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())
	_, err := http.Get(reqURL)
	if err != nil {
		fmt.Printf("[WHATSAPP ERROR] %v\n", err)
	}
}
