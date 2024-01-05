package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func main() {
	jobs := make(chan Job)
	doneJobs := make(chan Job)

	combos := getAll3LetterCombinations()
	jobCount := len(combos)

	go func(jobs chan<- Job, combos []string) {
		for _, str := range combos {
			jobs <- Job{name: str}
			fmt.Println(str)
		}
		close(jobs)
	}(jobs, combos)

	for i := 0; i < runtime.NumCPU(); i++ {
		go func(jobs <-chan Job, doneJobs chan<- Job, i int) {
			fmt.Println("Started goroutine", i)
			for {
				job, done := <-jobs
				if !done {
					break
				}
				fmt.Println("Got job", job.name)

				for !job.Do() {
				}
				doneJobs <- job
			}
		}(jobs, doneJobs, i)
	}

	users := []string{}
	for i := 0; i < jobCount; i++ {
		doneJob := <-doneJobs
		users = append(users, doneJob.uids...)
	}

	outFile, _ := os.Create("userids.txt")
	outFile.WriteString(strings.Join(users, "\n"))
}

type Job struct {
	name string
	uids []string
}

func (j *Job) Do() bool {
	req, err := http.Get(fmt.Sprintf("https://api.neos.com/api/users?name=%s", url.QueryEscape(j.name)))
	if err != nil {
		panic(err)
	}

	if req.StatusCode != http.StatusOK {
		if req.StatusCode == http.StatusTooManyRequests {
			secs, _ := strconv.Atoi(req.Header.Get("Retry-After"))
			time.Sleep(time.Second * time.Duration(secs))
		}
		return false
	}

	doc, _ := io.ReadAll(req.Body)

	uids := []UIDEntry{}

	json.Unmarshal(doc, &uids)

	j.uids = []string{}
	for _, uid := range uids {
		j.uids = append(j.uids, uid.Id)
	}

	return true
}

func getAll3LetterCombinations() []string {
	chars := []rune{}
	for i := 'a'; i <= 'z'; i++ {
		chars = append(chars, i)
	}

	combinations := []string{}
	for _, x := range chars {
		for _, y := range chars {
			for _, z := range chars {
				combinations = append(combinations, string([]rune{x, y, z}))
			}
		}
	}

	return combinations
}

type UIDEntry struct {
	Id string `json:"id"`
}
