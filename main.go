package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
)

type AccountResponse struct {
	Data struct {
		Token string `json:"token"`
	} `json:"data"`
}

type StationOverview struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ID                     string      `json:"id"`
		Serialnum              interface{} `json:"serialNum"`
		Userid                 interface{} `json:"userId"`
		Name                   interface{} `json:"name"`
		Powerstationtype       interface{} `json:"powerstationType"`
		Installationdate       interface{} `json:"installationDate"`
		Address                interface{} `json:"address"`
		Componentcapacity      interface{} `json:"componentCapacity"`
		Powerstationphotos     interface{} `json:"powerStationPhotos"`
		Installtime            interface{} `json:"installTime"`
		Runtype                int         `json:"runType"`
		Hassmartdevice         bool        `json:"hasSmartDevice"`
		Allinverteroffline     bool        `json:"allInverterOffline"`
		Status                 int         `json:"status"`
		Pac                    float64     `json:"pac"`
		Pmetertotal            float64     `json:"pmeterTotal"`
		Pacunit                string      `json:"pacUnit"`
		Pmetertotalunit        string      `json:"pmeterTotalUnit"`
		Carbondioxidereduction string      `json:"carbonDioxideReduction"`
		Planttrees             string      `json:"plantTrees"`
		Savecoal               string      `json:"saveCoal"`
		Soc                    interface{} `json:"soc"`
		Batteryp               interface{} `json:"batteryP"`
		Batteryporgion         interface{} `json:"batteryPOrgion"`
		Batterypunit           interface{} `json:"batteryPUnit"`
		Arrowmoduleinverter    int         `json:"arrowModuleInverter"`
		Arrowinvertergrid      int         `json:"arrowInverterGrid"`
		Arrowgridinverter      int         `json:"arrowGridInverter"`
		Arrowinverterload      int         `json:"arrowInverterLoad"`
		Arrowloadinverter      int         `json:"arrowLoadInverter"`
		Arrowbatteryinverter   interface{} `json:"arrowBatteryInverter"`
		Arrowinverterbattery   interface{} `json:"arrowInverterBattery"`
		Isgridbright           interface{} `json:"isGridBright"`
		Isbatterybright        interface{} `json:"isBatteryBright"`
		Cnstatus               interface{} `json:"cnStatus"`
		Statisticdata          interface{} `json:"statisticData"`
		Emonth                 float64     `json:"emonth"`
		Emonthunit             string      `json:"emonthUnit"`
		Eyear                  float64     `json:"eyear"`
		Eyearunit              string      `json:"eyearUnit"`
		Etotalunit             string      `json:"etotalUnit"`
		Eday                   float64     `json:"eday"`
		Edayunit               string      `json:"edayUnit"`
		Etotal                 float64     `json:"etotal"`
		Ploadunit              string      `json:"ploadUnit"`
		Pload                  float64     `json:"pload"`
	} `json:"data"`
	Time string `json:"time"`
}

type Storage struct {
	IsExcess bool    `json:"is_excess"`
	Excess   float64 `json:"excess"`
	Token    string  `json:"token"`
	Date     string  `json:"-"`
	Usage    float64 `json:"-"`
	Solar    float64 `json:"-"`
}

type Config struct {
	StationID  string `json:"station_id"`
	BotID      string `json:"bot_id"`
	Recipient  string `json:"recipient"`
	SheetyURL  string `json:"sheety_url"`
	SheetyAuth string `json:"sheety_auth"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	Salt       string `json:"salt"` //not sure if this changes
}

const SUNWAYS_ROOT = "https://www.sunways-portal.com"
const TGURL = "https://api.telegram.org"

func (overview StationOverview) Storage() Storage {
	data := overview.Data
	var storage Storage
	storage.Usage = data.Pload
	if data.Ploadunit == "W" {
		storage.Usage /= 1000
	}
	storage.Solar = data.Pac
	if data.Pacunit == "W" {
		storage.Solar /= 1000
	}
	storage.Excess = storage.Solar - storage.Usage
	storage.IsExcess = storage.Excess > -0.1
	storage.Date = overview.Time
	return storage
}

func (storage Storage) Message() string {
	if storage.Excess > -0.1 {
		return "☀️ Excess"
	}
	load := math.Abs(storage.Excess)
	return fmt.Sprintf("🔌 Insufficient: %.2fkW", load)
}

func (storage Storage) Save(path string) error {
	latest, err := json.Marshal(storage)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, latest, 0644)
}

func (storage Storage) Post(client *http.Client, sheetyURL, sheetyAuth string) error {
	// TODO: google sheets api
	// TODO: batch insert from history per time range
	row := map[string]interface{}{}
	row["date"] = storage.Date
	row["excessWatts"] = storage.Excess
	row["usageWatts"] = storage.Usage
	row["solarWatts"] = storage.Solar
	payload := map[string]interface{}{}
	payload["sheet1"] = row
	jsonValue, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", sheetyURL, bytes.NewReader(jsonValue))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	if sheetyAuth != "" {
		req.Header.Set("Authorization", sheetyAuth)
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = ioutil.ReadAll(res.Body)
	return err
}

func main() {
	// TODO: stop at night
	output := flag.String("o", "old.json", "output file")
	configPath := flag.String("c", "config.json", "config file")
	flag.Parse()
	config := parseConfig(*configPath)
	old, err := load(*output)
	if err != nil {
		fmt.Println(err)
		return
	}
	client := &http.Client{}
	storage, err := fetch(client, old.Token, config)
	if err != nil {
		fmt.Println(err)
		return
	}
	err = storage.Save(*output)
	if err != nil {
		fmt.Println(err)
	}
	if config.SheetyURL != "" {
		storage.Post(client, config.SheetyURL, config.SheetyAuth)
	}

	if storage.IsExcess == old.IsExcess {
		// no dif
		return
	}
	if config.BotID != "" && config.Recipient != "" {
		sendMessage(config.BotID, config.Recipient, storage.Message())
	}

}

func fetch(client *http.Client, token string, config Config) (Storage, error) {
	req, err := http.NewRequest("GET", SUNWAYS_ROOT+"/api/sys/curve/station/getSingleStationOverview?id="+config.StationID, nil)
	if err != nil {
		return Storage{}, err
	}
	req.Header.Set("Authorization", token)
	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return Storage{}, err
	}

	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return Storage{}, err
	}
	var overview StationOverview
	err = json.Unmarshal(body, &overview)
	if err != nil {
		return Storage{}, err
	}
	if overview.Data.Allinverteroffline {
		return Storage{}, fmt.Errorf("offline")
	}
	if overview.Code == "3010022" {
		// auth issue
		newToken, err := login(client, config.Email, config.Password, config.Salt)
		if err != nil {
			return Storage{}, err
		}
		return fetch(client, newToken, config)
	}
	// TODO: Handle other errors
	// TODO: notify if down for too long during the day
	storage := overview.Storage()
	storage.Token = token
	return storage, nil
}

func login(client *http.Client, email, password, salt string) (string, error) {
	payload := map[string]string{}
	payload["channel"] = "1"
	payload["email"] = email
	payload["password"] = password
	payload["salt"] = salt
	jsonValue, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest("POST", SUNWAYS_ROOT+"/api/sys/login/manager", bytes.NewReader(jsonValue))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	var account AccountResponse
	err = json.Unmarshal(body, &account)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	return account.Data.Token, nil

}

func parseConfig(path string) Config {
	configFile, err := os.Open(path)
	if err != nil {
		log.Fatal("Cannot open server configuration file: ", err)
	}
	defer configFile.Close()

	dec := json.NewDecoder(configFile)
	var config Config
	if err = dec.Decode(&config); errors.Is(err, io.EOF) {
		//do nothing
	} else if err != nil {
		log.Fatal("Cannot load server configuration file: ", err)
	}
	return config
}

func load(path string) (Storage, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return Storage{}, err
	}
	var storage Storage
	err = json.Unmarshal(content, &storage)
	return storage, err
}

// telegram
func constructPayload(chatID, message string) (*bytes.Reader, error) {
	payload := map[string]interface{}{}
	payload["chat_id"] = chatID
	payload["text"] = message
	payload["parse_mode"] = "markdown"
	payload["disable_web_page_preview"] = true

	jsonValue, err := json.Marshal(payload)
	return bytes.NewReader(jsonValue), err
}

func sendMessage(bot, chatID, message string) error {
	payload, err := constructPayload(chatID, message)
	if err != nil {
		fmt.Println(err)
		return err
	}
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/bot%s/sendMessage", TGURL, bot), payload)
	if err != nil {
		fmt.Println(err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		return err
	}
	defer res.Body.Close()
	_, err = ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}
