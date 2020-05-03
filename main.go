package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"

	_ "net/http/pprof"

	"github.com/google/uuid"
)

type FileMap struct {
	id   int
	path string
}

var (
	fileName        string
	fullUrlFile     string
	dirName         string
	downloadedFiles map[int]string
	parts           int
)

var lock = sync.RWMutex{}
var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
var fileSource = flag.String("fileSource", "", "the http origin of the file")

func main() {
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	fullUrlFile = *fileSource
	dirName = ".downloads"
	parts = 50
	os.Mkdir(dirName, 0777)
	buildFileName()
	downloadedFiles = make(map[int]string)
	putFile()
	merge()

	clean()

}

func downloadFileSize() int {
	response, err := http.Head(fullUrlFile)
	if err != nil {
		log.Println("Error while downloading", fullUrlFile, ":", err)
	}
	length, _ := strconv.Atoi(response.Header.Get("Content-Length"))
	sourceSize := int(length)

	return sourceSize
}

func putFile() {
	fileSize := downloadFileSize()
	fmt.Println(fileSize)
	i := 1
	var wg sync.WaitGroup
	wg.Add(parts)
	chanFiles := make(chan bool, parts)
	for i <= parts {
		go func(j int) {
			client := httpClient()
			request, err := http.NewRequest("GET", fullUrlFile, nil)
			if err != nil {
				log.Fatalln(err)
			}
			request.Header.Set("Range", fmt.Sprintf(`bytes=%v-%v`, ((j-1)*(fileSize/parts)), (((fileSize)/parts)*j)-1))
			resp, err := client.Do(request)

			checkError(err)

			defer resp.Body.Close()
			path := uuid.New().String()

			files := createFile(path)

			_, err = io.Copy(files, resp.Body)

			defer files.Close()

			lock.Lock()
			downloadedFiles[j] = path
			defer lock.Unlock()

			checkError(err)
			defer wg.Done()
			chanFiles <- true
		}(i)

		i = i + 1
	}

	wg.Wait()
	<-chanFiles
}

func buildFileName() {
	fileURL, err := url.Parse(fullUrlFile)
	checkError(err)

	path := fileURL.Path
	segments := strings.Split(path, "/")

	fileName = segments[len(segments)-1]
}

func httpClient() *http.Client {
	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
	}

	return &client
}

func createFile(fileName string) *os.File {
	file, err := os.Create(filepath.Join(dirName, filepath.Base(fileName)))
	checkError(err)
	return file
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func merge() {
	files := make([]string, 0)
	output := fileName
	out, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalln("failed to open outpout file:", err)
	}
	defer out.Close()
	for i := 1; i <= parts; i++ {
		files = append(files, dirName+"/"+downloadedFiles[i])
	}
	err = FilesToFile(fileName, 0777, "", files...)
	if err != nil {
		fmt.Println(err.Error())
	}
}

// FilesToBytes concatenate a list of files by the given delimiter.
// you can set a matching pattern to select the sources you want to process.
func FilesToBytes(del string, src ...string) ([]byte, error) {
	var tmp []byte

	// check := len(src) - 1
	for j, srcfile := range src {
		matches, err := filepath.Glob(srcfile)
		if err != nil {
			return tmp, err
		}
		totalMatches := len(matches)
		if totalMatches == 0 {
			return tmp, errors.New("cannot find " + srcfile)
		}
		for _, matchFiles := range matches {
			d, err := ioutil.ReadFile(matchFiles)
			if err != nil {
				return tmp, err
			}
			tmp = append(tmp, d...)
			if j < totalMatches-1 {
				fmt.Println(j)
			}
		}
	}
	return tmp, nil
}

// FilesToFile concatenate a list of files by the given delimiter
func FilesToFile(filename string, perm os.FileMode, del string, src ...string) error {
	con, err := FilesToBytes(del, src...)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filename, con, perm)
}

func clean() {
	// Open the directory and read all its files.
	dirRead, _ := os.Open(dirName)
	dirFiles, _ := dirRead.Readdir(0)

	// Loop over the directory's files.
	for index := range dirFiles {
		fileHere := dirFiles[index]

		// Get name of file and its full path.
		nameHere := fileHere.Name()
		fullPath := dirName + "/" + nameHere

		// Remove the file.
		os.Remove(fullPath)
		// fmt.Println("Removed file:", fullPath)
	}

	os.Remove(dirName)
}
