package main

import (
	"bytes"
	"container/list"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Page struct {
	Title string
	Body  []byte
}
type Directory struct {
	Title string
	Body  []string
}
type Drive struct {
	Title string
	Body  []string
}

func main() {

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	ws := make(chan int)

	go setupWebServer(ws, port)

	x := <-ws

	fmt.Println(x, x)
}

var (
	templates = template.Must(template.ParseFiles("edit.html", "view.html", "directories.html", "drives.html"))

	validPath = regexp.MustCompile("^/(edit|save|view|directories|drives)/([a-zA-Z0-9]+)$")
)

func setupWebServer(c chan int, port string) {

	http.HandleFunc("/view/", makeHandler(viewHandler))
	http.HandleFunc("/edit/", makeHandler(editHandler))
	http.HandleFunc("/save/", makeHandler(saveHandler))

	http.HandleFunc("/execCommand/", makeHandler(execHandler))
	http.HandleFunc("/drives/", makeHandler(getDrives))
	http.HandleFunc("/directories/", makeHandler(getDirectories))

	log.Printf("Listening on port %s", port)
	log.Printf("Open 'http://localhost:%s/drives/all' in the browser", port)

	if err := http.ListenAndServe(fmt.Sprintf(":%s", port), nil); err != nil {
		log.Fatal(err)
	}

	c <- 200
}

func makeHandler(fn func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		m := validPath.FindStringSubmatch(r.URL.Path)

		if m == nil {

			http.NotFound(w, r)

			return
		}
		fn(w, r, m[2])
	}

}

/*Directory Retrieval Methods*/

func getDirectories(writer http.ResponseWriter, request *http.Request, title string) {

	dirs, ok := request.URL.Query()["dir"]

	if !ok || len(dirs) < 1 {

		log.Println("Url Param 'dir' is missing")

		return

	}

	var fsItems = getDirectoriesImpl(dirs, 1000, 1)

	directories := make([]string, fsItems.Len(), fsItems.Len()+1)

	var idx int = 0
	for e := fsItems.Front(); e != nil; e = e.Next() {

		directories[idx] = fmt.Sprintf("%s", e.Value)

		idx++

	}

	renderDirectoryTemplate(writer, "directories", &Directory{Title: "View FileSystem Directories", Body: directories})
}

func getDrives(writer http.ResponseWriter, request *http.Request, title string) {

	var drvs = getDrivesImpl()

	renderDriveTemplate(writer, "drives", &Drive{Title: "View FileSystem Drives", Body: drvs})
}

/*Handlers*/

func execHandler(writer http.ResponseWriter, request *http.Request, title string) {

	cmds, ok := request.URL.Query()["cmd"]
	if !ok || len(cmds) < 1 {
		fmt.Fprintln(writer, fmt.Sprintf("Execute Command Failed with Error: %s.\n", "url Param 'cmd' is missing"), nil)
	}

	var stdError string
	var stdOut string
	var err error
	for cmd := range cmds {
		err, stdOut, stdError = executeShellCommand(string(cmd))
		if err != nil {
			fmt.Fprintln(writer, fmt.Sprintf("Execute Command Failed with Error: %s : %s.\n", stdError, err), nil)
		}
		fmt.Fprintln(writer, fmt.Sprintf("Executed Command Successfully: %s.\n", stdOut), nil)
	}

}

func viewHandler(writer http.ResponseWriter, r *http.Request, title string) {

	p, err := loadPage(title)

	if err != nil {

		http.Redirect(writer, r, "/edit/"+title, http.StatusFound)

		return

	}
	renderPageTemplate(writer, "view", p)
}

func editHandler(writer http.ResponseWriter, r *http.Request, title string) {

	p, err := loadPage(title)

	if err != nil {

		p = &Page{Title: title}

	}

	renderPageTemplate(writer, "edit", p)

}

func saveHandler(writer http.ResponseWriter, r *http.Request, title string) {

	body := r.FormValue("body")

	p := &Page{Title: title, Body: []byte(body)}

	var err = p.save()

	if err != nil {

		http.Error(writer, err.Error(), http.StatusInternalServerError)

		return
	}

	http.Redirect(writer, r, "/view/"+title, http.StatusFound)
}

/*Directory Methods Implementation*/

func enableCors(writer *http.ResponseWriter) {

	(*writer).Header().Set("Access-Control-Allow-Origin", "*")

}

func initialize(writer http.ResponseWriter, r *http.Request) bool {

	enableCors(&writer)

	if r.Method != "GET" {
		http.Error(writer, "Method is not supported.", http.StatusNotFound)
		return false
	}

	return true
}

func getDrivesImpl() (r []string) {

	for _, drive := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		f, err := os.Open(string(drive) + ":\\")
		if err == nil {
			r = append(r, string(drive))
			f.Close()
		}

	}
	return
}

func getDirectoriesImpl(dirs []string, maxDirs int, depth int) *list.List {

	fileList := list.New()
	var numDirsMax = 0

	for _, dir := range dirs {

		fsItems, err := ioutil.ReadDir(string(dir))

		if err != nil {

			log.Fatal(err)

		}

		for _, fsItem := range fsItems {

			fqn := string(dir) + fsItem.Name()

			if numDirsMax > maxDirs {

				break

			} else if fsItem.IsDir() && depth > 1 {

				var directoryList = readDirectory(fqn, depth)

				for directory := range directoryList {

					fileList.PushBack(directory)

				}

			} else {

				fileList.PushBack(fqn)
			}
			numDirsMax++
		}

	}
	return fileList
}

func readDirectory(d string, depth int) []string {

	var files []string

	root := d

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {

		if strings.Count(path, "\\") > depth {

			return nil

		} else {

			files = append(files, path)

			return nil
		}

	})

	if err != nil {

		panic(err)

	}

	return files
}

/*Template Handling Methods*/

func getTitle(writer http.ResponseWriter, r *http.Request) (string, error) {

	m := validPath.FindStringSubmatch(r.URL.Path)

	if m == nil {

		http.NotFound(writer, r)

		return "", errors.New("invalid Page Title")

	}

	return m[2], nil // The title is the second subexpression.
}

func renderPageTemplate(writer http.ResponseWriter, tmpl string, p *Page) {

	err := templates.ExecuteTemplate(writer, tmpl+".html", p)

	if err != nil {

		http.Error(writer, err.Error(), http.StatusInternalServerError)

	}

}

func renderDirectoryTemplate(writer http.ResponseWriter, tmpl string, d *Directory) {

	err := templates.ExecuteTemplate(writer, tmpl+".html", d)

	if err != nil {

		http.Error(writer, err.Error(), http.StatusInternalServerError)

	}

}

func renderDriveTemplate(writer http.ResponseWriter, tmpl string, d *Drive) {

	err := templates.ExecuteTemplate(writer, tmpl+".html", d)

	if err != nil {

		http.Error(writer, err.Error(), http.StatusInternalServerError)

	}

}

func loadPage(title string) (*Page, error) {

	filename := title + ".txt"

	body, err := ioutil.ReadFile(filename)

	if err != nil {

		return nil, err

	}

	return &Page{Title: title, Body: body}, nil
}

func (p *Page) save() error {

	filename := p.Title + ".txt"

	return ioutil.WriteFile(filename, p.Body, 0600)

}

/* Shell Command Handler*/

const ShellToUse = "bash"

func executeShellCommand(command string) (error, string, string) {

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(ShellToUse, "-c", command)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return err, stdout.String(), stderr.String()
}
