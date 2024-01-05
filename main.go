package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	userID     = flag.String("u", "", "UserID of user to mine meteors for.")
	userIDFile = flag.String("f", "", "Newline delimited file of userIDs to mine for.")
)

var (
	ErrBadStatus          = errors.New("got bad status code")
	ErrUserNotFound       = errors.New("user not found")
	ErrDatabaseIssue      = errors.New("database issue")
	ErrUnknownError       = errors.New("unknown error")
	ErrUnknownMeteor      = errors.New("unknown meteor")
	ErrMeteorAlreadyMined = errors.New("meteor already mined")
)

var client = http.Client{
	Timeout: time.Second * 10,
}

func main() {
	flag.Parse()
	if *userID == "" && *userIDFile == "" {
		flag.Usage()
		return
	}

	userIDs := []string{}

	if *userID != "" {
		userIDs = append(userIDs, *userID)
	}

	if *userIDFile != "" {
		log.Printf("Reading UserIDs from %s", *userIDFile)

		userIDsFromFile, err := getUserIDsFromFile(*userIDFile)
		if err != nil {
			log.Printf("Failed to get UserID file: %s", err)
		} else {
			log.Printf("Got %d UserIDs", len(userIDsFromFile))
			userIDs = append(userIDs, userIDsFromFile...)
		}
	}

	getMeteorsCount := runtime.NumCPU() / 4
	mineMeteorsCount := runtime.NumCPU() - getMeteorsCount

	if getMeteorsCount < 0 || mineMeteorsCount < 0 {
		panic("Unable to assign enough goroutines to mining/getting.")
	}

	jobQueueGetMeteors := make(chan JobGetUserMeteors, getMeteorsCount)
	jobQueueMineUserMeteor := make(chan JobMineUserMeteor, mineMeteorsCount)

	for i := 0; i < getMeteorsCount; i++ {
		go func(jobQueueGetMeteors <-chan JobGetUserMeteors, jobQueueMineUserMeteor chan<- JobMineUserMeteor) {
			for {
				job := <-jobQueueGetMeteors
				log.Println("Got get meteor job for", job.user)
				meteors, err := job.Do()
				if err != nil {
					log.Println("Got error while getting meteors:", err)
					continue
				}
				for _, meteor := range meteors {
					jobQueueMineUserMeteor <- JobMineUserMeteor{user: job.user, id: meteor.Id}
				}
			}
		}(jobQueueGetMeteors, jobQueueMineUserMeteor)
	}

	for i := 0; i < mineMeteorsCount; i++ {
		go func(jobQueueMineUserMeteor <-chan JobMineUserMeteor) {
			for {
				job := <-jobQueueMineUserMeteor
				log.Println("Got mine meteor id", job.id, "job for", job.user)

				err := job.Do()
				if err != nil {
					log.Println("Got error while mining meteor:", err)
				} else {
					log.Printf("Mined meteor %d for %s", job.id, job.user)
				}
			}
		}(jobQueueMineUserMeteor)
	}

	ticker := time.Tick(time.Hour)
	for {
		for _, user := range userIDs {
			log.Println("Attempting to mine for", user)
			jobQueueGetMeteors <- JobGetUserMeteors{user: user}
		}
		<-ticker
	}
}

func getUserIDsFromFile(file string) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	text, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	users := strings.Split(string(text), "\n")
	result := []string{}
	for _, str := range users {
		if str == "" {
			continue
		}
		result = append(result, strings.TrimSpace(str))
	}

	return result, err
}

type JobGetUserMeteors struct {
	user string
}

func (j *JobGetUserMeteors) Do() ([]Meteor, error) {
	meteors, err := getMeteors(j.user)
	if err != nil {
		return nil, err
	}

	log.Printf("Found %d meteor(s)", len(meteors))

	return meteors, nil
}

type JobMineUserMeteor struct {
	user string
	id   int
}

func (j *JobMineUserMeteor) Do() error {
	apiURL := fmt.Sprintf("https://account.neos.com/v1/meteors/%s/mined/%d", j.user, j.id)

	req, err := http.NewRequest("POST", apiURL, nil)
	if err != nil {
		return err
	}

	req.Header.Add("User-Agent", "Neos/2022.1.28.1335")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	number, err := strconv.Atoi(string(body))
	if err != nil {
		log.Print(string(body))
		return err
	}

	switch number {
	case -1:
		return ErrUnknownMeteor
	case 0:
		return ErrMeteorAlreadyMined
	case 1:
		return nil
	default:
		return ErrUnknownError
	}
}

func getMeteors(user string) ([]Meteor, error) {
	res, err := client.Get(fmt.Sprintf("https://account.neos.com/v1/meteors/%s", url.PathEscape(user)))
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, ErrBadStatus
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	number, err := strconv.Atoi(string(body))
	if err == nil {
		// omg it's an error!
		switch number {
		case -1:
			return nil, ErrUserNotFound

		case -2:
			return nil, ErrDatabaseIssue

		default:
			return nil, ErrUnknownError
		}
	}

	var parsedJson struct {
		Meteors []Meteor `json:"meteors"`
	}
	err = json.Unmarshal(body, &parsedJson)
	if err != nil {
		return nil, err
	}

	return parsedJson.Meteors, nil
}

type Meteor struct {
	Id      int `json:"id"`
	Nuggets int `json:"nuggets"`
}
