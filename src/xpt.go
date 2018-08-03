package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func main() {
	// Согласно нашей структуре каталогов xpt лежит в sandbox/etc/xpt.
	sandbox, _ := filepath.Abs(filepath.Join(filepath.Dir(os.Args[0]), "..", ".."))
	fmt.Println("sandbox=" + sandbox)

	// Настраиваем cache-директорию.
	cache := setupCacheDirectory(sandbox)

	//
	// Обрабатываем аргументы командной строки.
	//

	if len(os.Args) == 2 && os.Args[1] == "update" {
		os.Exit(update(sandbox))
	} else if len(os.Args) > 2 && os.Args[1] == "install" {
		os.Exit(install(sandbox, cache))
	} else {
		os.Exit(usage())
	}
}

func setupCacheDirectory(sandbox string) string {
	cache := os.Getenv("XPTCACHE")
	if cache == "" { // Если XPTCACHE не задан, то используем домашний каталог.
		u, _ := user.Current()
		cache = filepath.Join(u.HomeDir, "xptcache")
	}
	if _, err := os.Stat(cache); os.IsNotExist(err) {
		os.MkdirAll(cache, os.ModePerm)
	}
	return cache
}

func update(sandbox string) int {
	sourcesTxt := filepath.Join(sandbox, "etc", "xpt", "sources.txt")
	fmt.Println("sourcesTxt=" + sourcesTxt)
	dat, err := ioutil.ReadFile(sourcesTxt)
	if err != nil {
		fmt.Println("*** Error: sources.txt not found: " + sourcesTxt)
		os.Exit(1)
	}

	updateTxtContent := ""

	updateOne := func(url string, tag string) string {
		repoURL := url
		if tag != "notag" {
			repoURL += "/" + tag
		}
		packagesTxtURL := repoURL + "/packages.txt"
		packagesTxt, err := downloadFile(packagesTxtURL)
		if err != nil {
			fmt.Println("!!! Warning: ")
		}
		updateTxtContentPart := ""
		for _, line := range strings.Split(packagesTxt, "\n") {
			packageFileName := stripCtlAndExtFromUTF8(line)
			if line == "" {
				continue
			}
			packageURL := repoURL + "/" + packageFileName
			packageName := strings.SplitN(packageFileName, "_", 2)[0]
			updateTxtContentPart += tag + " " + packageName + " " + packageURL + "\n"
		}
		return updateTxtContentPart
	}
	fmt.Println("--- read sources.txt")
	for _, line := range strings.Split(string(dat), "\n") {
		line = stripCtlAndExtFromUTF8(line)
		if strings.HasPrefix(line, "repo ") {
			// Удаляем двойные пробелы, т.к. строка может быть отформатирована разным кол-вом пробелов для наглядности sources.txt.
			re := regexp.MustCompile(`[\s\p{Zs}]{2,}`)
			line = re.ReplaceAllString(line, " ")
			fmt.Println("--- " + line)
			words := strings.Split(line, " ") // Массив вида [repo http://url tag1 tag2].
			url := words[1]
			tags := words[2:]
			if len(tags) == 0 { // Репозиторий без тэга.
				updateTxtContent += updateOne(url, "notag")
			} else { // Репозиторий с тегом/тегами.
				for _, tag := range tags {
					updateTxtContent += updateOne(url, tag)
				}
			}
		}
		fmt.Println("--- updateTxtContent=" + updateTxtContent)
	}

	os.MkdirAll(filepath.Join(sandbox, "var", "xpt"), os.ModePerm)
	f, e := os.Create(filepath.Join(sandbox, "var", "xpt", "update.txt"))
	if e != nil {
		panic(e)
	}
	defer f.Close()
	f.WriteString(updateTxtContent)
	f.Sync()
	return 0
}

func install(sandbox string, cache string) int {
	var names []string // Массив названий пакетов для установки.
	tag := ""          // Тэг этих пакетов.
	for _, arg := range os.Args[2:] {
		if arg == "@" {
			tag = os.Args[len(os.Args)-1]
			break
		} else {
			names = append(names, arg)
		}
	}
	fmt.Printf("--- names: %v\n", names)
	fmt.Printf("--- tag: %s\n", tag)

	var db [][]string // Считаем из update.txt в виде [[tag1 package1 url1], [tag2 package2 url2]].
	updateTxt := filepath.Join(sandbox, "var", "xpt", "update.txt")
	dat, err := ioutil.ReadFile(updateTxt)
	if err != nil {
		fmt.Println("*** Error: update.txt not found: " + updateTxt)
		os.Exit(1)
	}
	for _, line := range strings.Split(string(dat), "\n") {
		splits := strings.Split(line, " ")
		if len(splits) == 3 {
			db = append(db, splits)
		}
	}

	for _, name := range names {
		installOne(sandbox, cache, name, tag, db)
	}
	return 0
}

func installOne(sandbox string, cache string, name string, tag string, db [][]string) {
	fmt.Println("--- installOne: " + name)
	var urls []string
	for _, check := range db {
		if check[0] == tag && check[1] == name {
			urls = append(urls, check[2])
		}
	}
	if len(urls) > 1 {
		fmt.Printf("*** Error: More than one url for a package: %v\n", urls)
		os.Exit(1)
	}
	fmt.Println(cache)
}

type xptPackage struct {
	name string
	tag  string
	url  string
	size string
}

func usage() int {
	fmt.Println("xpt ver. 0.0.0 (" + runtime.Version() + ")")
	fmt.Println("usage: xpt install package1 package2 @ tag")
	return 1
}

// Функция удаляет все непечатаемые символы из строки.
func stripCtlAndExtFromUTF8(str string) string {
	return strings.Map(func(r rune) rune {
		if r >= 32 && r < 127 {
			return r
		}
		return -1
	}, str)
}

// TODO
func downloadFile(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	buf := bytes.NewBuffer(nil)
	_, err = io.Copy(buf, resp.Body)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

/*
//https://golangcode.com/download-a-file-with-progress/
func DownloadFile(filepath string, url string) error {

	// Create the file, but give it a tmp file extension, this means we won't overwrite a
	// file until it's downloaded, but we'll remove the tmp extension once downloaded.
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create our progress reporter and pass it to be used alongside our writer
	//counter := &WriteCounter{}
	//_, err = io.Copy(out, io.TeeReader(resp.Body, counter))
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	// The progress use the same line so print a new line once it's finished downloading
	//fmt.Print("\n")

	return nil
}*/
