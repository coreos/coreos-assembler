package auth

import (
	"fmt"
	"net/http"
	"os/user"
	"path/filepath"

	"code.google.com/p/goauth2/oauth"
)

// client registered under 'marineam-tools'
var config = oauth.Config{
	ClientId:     "937427706989-nbndmfkp0knqardoagk6lbcamrsh828i.apps.googleusercontent.com",
	ClientSecret: "F6Xs5wGHZzGw-QFXl3aylLUT",
	Scope:        "https://www.googleapis.com/auth/devstorage.full_control",
	AuthURL:      "https://accounts.google.com/o/oauth2/auth",
	TokenURL:     "https://accounts.google.com/o/oauth2/token",
	RedirectURL:  "urn:ietf:wg:oauth:2.0:oob",
}

func GoogleClient() (*http.Client, error) {
	userInfo, err := user.Current()
	if err != nil {
		return nil, err
	}

	cachePath := filepath.Join(userInfo.HomeDir, ".gsextra-google.json")
	transport := oauth.Transport{Config: &config}
	transport.TokenCache = oauth.CacheFile(cachePath)
	token, _ := transport.TokenCache.Token()
	if token == nil {
		var code string
		fmt.Printf("Go to URL: %s\nAuth Code: ", transport.AuthCodeURL(""))
		if _, err = fmt.Scanln(&code); err != nil {
			return nil, err
		}
		if _, err = transport.Exchange(code); err != nil {
			return nil, err
		}
	} else {
		transport.Token = token
	}

	return transport.Client(), nil
}
