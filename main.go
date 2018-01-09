// Pulls down all NPM packages
package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"sync"
	"time"
)

var registry = ""
var numberOfWorkers = 0
var limit = 0
var secondsUntilFlush = 0

type Version struct {
	Shasum  string `json:"shasum"`
	Tarball string `json:"tarball"`
}

type Row struct {
	ID    string  `json:"id"`
	Value Version `json:"value"`
}

type Response struct {
	TotalRows int   `json:"total_rows"`
	Offset    int   `json:"offset"`
	Rows      []Row `json:"rows"`
}

var myClient = &http.Client{Timeout: 60 * time.Second}

func getJson(url string, target interface{}) error {
	r, err := myClient.Get(url)
	if err != nil {
		return err
	}
	defer func() {
		err := r.Body.Close()
		if err != nil {
			panic(err)
		}
	}()

	return json.NewDecoder(r.Body).Decode(target)
}

func downloadFile(filepath string, url string, shasum string) (err error) {
	// Check if file exists first, skip if shasum matches
	// Create the file
	if _, err := os.Stat(filepath); err == nil {
		sha := sha1.New()
		f, err := os.Open(filepath)
		if err != nil {
			log.Fatal(err)
		}
		defer func() {
			err := f.Close()
			if err != nil {
				panic(err)
			}
		}()
		if _, err := io.Copy(sha, f); err != nil {
			log.Fatal(err)
		}
		hashStr := hex.EncodeToString(sha.Sum(nil)[:])
		if hashStr != shasum {
			fmt.Println("Stop the press " + url + " had invalid shasum")
			fmt.Println("Expected: " + shasum)
			fmt.Println("Got: " + hashStr)
		}
		return nil
	} else {
		out, err := os.Create(filepath)
		if err != nil {
			return err
		}
		defer func() {
			err := out.Close()
			if err != nil {
				panic(err)
			}
		}()

		// Get the data
		resp, err := http.Get(url)
		if err != nil {
			return err
		}
		defer func() {
			err := resp.Body.Close()
			if err != nil {
				panic(err)
			}
		}()

		sha := sha1.New()
		bodyReader := io.TeeReader(resp.Body, sha)

		// Writer the body to file
		_, err = io.Copy(out, bodyReader)
		if err != nil {
			return err
		}

		hashStr := hex.EncodeToString(sha.Sum(nil)[:])
		if hashStr != shasum {
			fmt.Println("Stop the press " + url + " had invalid shasum")
			fmt.Println("Expected: " + shasum)
			fmt.Println("Got: " + hashStr)
		}
		return nil
	}
}

func printLog(id int, version Version, verb string) {
	// idStr := strconv.Itoa(id)
	// fmt.Println("[" + idStr + "] " + verb + " " + version.Tarball + "=" + version.Shasum)
}

func doWork(id int, version Version) {
	printLog(id, version, "downloading")
	filename := path.Base(version.Tarball)
	err := downloadFile("downloads/"+filename, version.Tarball, version.Shasum)
	if err != nil {
		panic(err)
	}
}

func main() {

	numberOfWorkers := flag.Int("workers", 100, "how many workers to use")
	limit := flag.Int("limit", 5000, "how many docs to grab per request")
	secondsUntilFlush := flag.Int("flush-interval", 60, "how often (in seconds) to run ipfs files flush")
	registry := flag.String("registry", "http://54.183.196.81:5984/npm-registry", "what registry to use")

	flag.Parse()
	fmt.Println("Using registry " + *registry)

	wg := &sync.WaitGroup{}
	proccessedJobs := 0
	totalNumberOfJobs := 0

	previousProcessedJobs := 0

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(1 * time.Second)
				totalNumberOfJobsStr := strconv.Itoa(totalNumberOfJobs)
				fmt.Println("Fetched " + totalNumberOfJobsStr)
			}
		}
	}()

	// Get list of packages and versions
	lastID := ""
	jobs := make([]Version, 0)
	for {
		res := Response{}
		limitStr := strconv.Itoa(*limit)
		url := *registry + "/_design/latest_main/_view/latest-version-only?limit=" + limitStr
		if lastID != "" {
			url = url + "&start_key=\"" + lastID + "\"&skip=1"
		}
		err := getJson(url, &res)
		if err != nil {
			panic(err)
		}

		// Grab all versions from all fetched packages
		if len(res.Rows) == 0 {
			break
		} else {
			lastID = res.Rows[len(res.Rows)-1].ID
		}
		for _, r := range res.Rows {
			wg.Add(1)
			totalNumberOfJobs = totalNumberOfJobs + 1
			jobs = append(jobs, r.Value)
		}
	}
	fmt.Println("Finished processing versions")
	cancel()
	go func() {
		for {
			time.Sleep(1 * time.Second)
			diff := proccessedJobs - previousProcessedJobs
			fmt.Println("## Completed: " + strconv.Itoa(proccessedJobs) + "/" + strconv.Itoa(totalNumberOfJobs) + " (" + strconv.Itoa(diff) + " pkgs/second)")
			previousProcessedJobs = proccessedJobs
		}
	}()
	jobsChannel := make(chan Version, 0)
	for i := 1; i <= *numberOfWorkers; i++ {
		go func(i int) {
			for {
				version := <-jobsChannel
				printLog(i, version, "started")
				// check if filename already exists in Files API
				filename := path.Base(version.Tarball)
				cmd := exec.Command("ipfs", "files", "stat", "/npm-registry/"+filename)
				_, err := cmd.CombinedOutput()
				if err != nil {
					doWork(i, version)
					printLog(i, version, "downloaded")
					cmd = exec.Command("sh", "-c", "cat downloads/"+filename+" | ipfs files write --flush=false --create /npm-registry/"+filename)
					_, err := cmd.CombinedOutput()
					if err != nil {
						fmt.Println("Make sure daemon is running")
						panic(err)
					}
					printLog(i, version, "completed")
					proccessedJobs = proccessedJobs + 1
					wg.Done()
				} else {
					proccessedJobs = proccessedJobs + 1
					wg.Done()
				}
			}
		}(i)
	}
	go func() {
		for {
			if len(jobs) == 0 {
				break
			}
			version := jobs[0]
			jobs = jobs[:0+copy(jobs[0:], jobs[1:])]
			jobsChannel <- version
		}
	}()
	// version := jobs[0]
	// jobs = jobs[:0+copy(jobs[0:], jobs[1:])]
	go func() {
		for {
			time.Sleep(time.Second * time.Duration(*secondsUntilFlush))
			fmt.Println("Flushing...")
			cmd := exec.Command("sh", "-c", "ipfs files flush")
			_, err := cmd.CombinedOutput()
			if err != nil {
				panic(err)
			}
			fmt.Println("Flushed")
		}
	}()
	wg.Wait()
	fmt.Println("All done")
	fmt.Println("Final Flush")
	cmd := exec.Command("sh", "-c", "ipfs files flush")
	_, err := cmd.CombinedOutput()
	if err != nil {
		panic(err)
	}
	fmt.Println("Flushed")
}
