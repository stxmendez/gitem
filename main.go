package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/golang/gddo/httputil/header"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

type ReposResponse []map[string]interface{}

func execGitCommand(index, total int, dirPath, gitUrl string, args ...string) error {
	log.Printf("%d/%d - %s %s from %s\n", index, total, args[0], dirPath, gitUrl)
	cwd, err := syscall.Getwd()
	if err != nil {
		return err
	}
	err = syscall.Chdir(dirPath)
	if err != nil {
		return err
	}
	gitCmd := exec.Command("git", args...)
	gitIn, _ := gitCmd.StdinPipe()
	gitOut, _ := gitCmd.StdoutPipe()
	gitCmd.Start()
	gitIn.Close()
	buf, err := ioutil.ReadAll(gitOut)
	if err != nil {
		return err
	}
	err = gitCmd.Wait()
	if err != nil {
		fmt.Print(string(buf))
	}
	syscall.Chdir(cwd)
	return err
}

func fetchRepos(debug bool, url, userName, password string) (nextPage string, response ReposResponse, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}

	req.SetBasicAuth(userName, password)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		err = errors.New(resp.Status)
		return
	}

	params := header.ParseList(resp.Header, "Link")
	if len(params) > 0 {
		for _, param := range params {
			if strings.Index(param, "rel=\"next\"") != -1 {
				s, e := strings.Index(param, "<"), strings.Index(param, ">")
				if s != -1 && e != -1 {
					nextPage = param[s+1 : e]
				}
			}
		}
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	if debug {
		var out bytes.Buffer
		json.Indent(&out, respBody, "", "\t")
		out.WriteTo(os.Stdout)
	}

	err = json.Unmarshal(respBody, &response)

	return nextPage, response, err
}

func main() {
	var debug bool
	var userName, password, githubOrgName, rootPath string
	flag.BoolVar(&debug, "debug", false, "Print debug logging information")
	flag.StringVar(&rootPath, "rootPath", "", "Root path containing checked out subdirectories")
	flag.StringVar(&userName, "username", "", "Username")
	flag.StringVar(&password, "password", "", "Password")
	flag.StringVar(&githubOrgName, "githubOrg", "", "Git organization name")
	flag.Parse()

	if rootPath == "" {
		log.Fatal(errors.New("rootPath must be specified"))
	}

	if userName == "" {
		log.Fatal(errors.New("username must be specified"))
	}

	if password == "" {
		log.Fatal(errors.New("password must be specified"))
	}

	if githubOrgName == "" {
		log.Fatal(errors.New("Github orgname must be specified"))
	}

	// Fetch the initial set of repos and continue until no more links are returned.
	url := fmt.Sprintf("https://api.github.com/orgs/%s/repos?per_page=200&type=all", githubOrgName)
	var aggregatedResponse ReposResponse
	for nextPage := url; nextPage != ""; {
		var response ReposResponse
		var err error
		nextPage, response, err = fetchRepos(debug, nextPage, userName, password)
		if err != nil {
			log.Fatal(err)
		}

		aggregatedResponse = append(aggregatedResponse, response...)
	}

	if err := processResponse(aggregatedResponse, githubOrgName, rootPath); err != nil {
		log.Fatal(err)
	}
}

func processResponse(response ReposResponse, githubOrgName, rootPath string) error {
	log.Printf("Processing %d repos for org %s\n", len(response), githubOrgName)
	total := len(response)
	for index, e := range response {
		gitUrl := e["clone_url"].(string)

		if !strings.HasSuffix(gitUrl, ".git") {
			log.Fatal(errors.New("Missing '.git' from URL.."))
		}

		i := strings.LastIndex(gitUrl, "/")
		if i == -1 {
			log.Fatal(errors.New("Invalid url format..."))
		}

		dirName := gitUrl[i+1 : len(gitUrl)-4]
		dirPath := fmt.Sprintf("%s%s%s", rootPath, string(os.PathSeparator), dirName)
		fi, err := os.Stat(dirPath)
		if err != nil {
			// Doesn't exist, checkout
			err := execGitCommand(index, total, rootPath, gitUrl, "clone", gitUrl)
			if err != nil {
				log.Println(err)
			}
		} else if fi.IsDir() {
			err := execGitCommand(index, total, dirPath, gitUrl, "pull", "origin")
			if err != nil {
				log.Println(err)
			}
		} else {
			log.Fatal(errors.New("Expected directory"))
		}
	}

	return nil
}