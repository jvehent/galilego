package main

import (
	"fmt"
	"github.com/bradfitz/http2"
	"github.com/gorilla/mux"
	"github.com/nfnt/resize"
	"image"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

func main() {
	var srv http.Server
	srv.Addr = "0.0.0.0:8064"

	r := mux.NewRouter()

	r.HandleFunc("/", home).Methods("GET")
	r.HandleFunc("/gallery/{galpath:.*}", serveGallery).Methods("GET")

	fs := http.FileServer(http.Dir(`./statics`))
	r.Handle("/statics/{staticfile}", http.StripPrefix("/statics", fs)).Methods("GET")

	http.Handle("/", r)
	http2.ConfigureServer(&srv, &http2.Server{})
	log.Fatal(srv.ListenAndServeTLS("server.crt", "server.key"))
}

func home(w http.ResponseWriter, r *http.Request) {
	// The "/" pattern matches everything, so we need to check
	// that we're at the root here.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	galHtml := genGalleryHtml("gallery")
	io.WriteString(w, `<html>
	<head><title>Galilego HTTP/2 web gallery</title>
	<body>
		<h1>Content of <a href="/">/</a></h1>
`+galHtml+`
	</body></html>`)
}

func homeOldHTTP(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, `<html><body>
	<h1>Galilego is a HTTP/2 web gallery.</h1>
	<p>Unfortunately, you're <b>not</b> using HTTP/2 right now. To do so download and install the latest Firefox from <a href="https://www.mozilla.org">https://www.mozilla.org</a>.</p>
</body></html>`)
}

var imgre = regexp.MustCompile(`(?i).*\.(jpe?g|png|gif)$`)

func serveGallery(w http.ResponseWriter, r *http.Request) {
	var err error
	vars := mux.Vars(r)
	galpath := "gallery/" + vars["galpath"]
	log.Println("requested " + galpath)
	if imgre.MatchString(galpath) {
		width := uint64(0)
		if _, ok := r.URL.Query()["width"]; ok {
			width, err = strconv.ParseUint(r.URL.Query()["width"][0], 10, 64)
		}
		if err != nil {
			log.Println(err)
		}
		img, err := getImage(galpath, uint(width))
		if err != nil {
			log.Println(err)
		}
		http.ServeContent(w, r, galpath, time.Now(), img)
		img.Close()
	} else {
		galHtml := genGalleryHtml(galpath)
		io.WriteString(w, `<html>
		<head><title>Galilego HTTP/2 web gallery</title>
		<body>
			<h1>Content of <a href="`+r.RequestURI+`">`+r.RequestURI+`</a></h1>
	`+galHtml+`
		</body></html>`)
	}
}

// genGalleryHtml reads the content of path and returns HTML code that
// represents the gallery
func genGalleryHtml(path string) string {
	var (
		dirHtml, imgHtml string
	)
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("<p>Error: %v</p>", err)
	}
	if !fi.Mode().IsDir() {
		return `<p>Error: ` + path + ` is not a valid directory</p>`
	}
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("<p>Error: %v</p>", err)
	}
	defer dir.Close()
	dirContent, err := dir.Readdir(-1)
	if err != nil {
		return fmt.Sprintf("<p>Error: %v</p>", err)
	}
	for _, dirEntry := range dirContent {
		if dirEntry.IsDir() {
			// if the entry is a folder, add a folder icon
			dirHtml += fmt.Sprintf("<a href=\"/%s/%s\"><img src=\"/statics/f.jpg\" alt=\"%s\"/>%s</a>\n",
				path, dirEntry.Name(), dirEntry.Name(), dirEntry.Name())
		} else if dirEntry.Mode().IsRegular() && imgre.MatchString(dirEntry.Name()) {
			// if the entry is an image, display its miniature
			imgHtml += fmt.Sprintf("<a href=\"/%s/%s\"><img src=\"/%s/%s?width=300\" width=\"300\"/></a>\n",
				path, dirEntry.Name(), path, dirEntry.Name())
		}
	}
	return fmt.Sprintf("%s\n%s\n", dirHtml, imgHtml)
}

func getImage(path string, size uint) (fd *os.File, err error) {
	if size == 0 {
		// if size is zero, serve the file directly
		fd, err = os.Open(path)
		if err != nil {
			return nil, err
		}
		return
	}
	cachedPath := fmt.Sprintf("imgcache/%s_%d", path, size)
	_, err = os.Stat(cachedPath)
	if err != nil {
		// just in case the directory doesn't exist yet...
		os.MkdirAll(filepath.Dir(cachedPath), 0755)

		// generate the cached file
		fd, err = os.Open(path)
		if err != nil {
			return
		}

		// decode jpeg into image.Image
		var img image.Image
		img, err = jpeg.Decode(fd)
		if err != nil {
			return
		}
		fd.Close()

		// resize to width 1000 using Lanczos resampling
		// and preserve aspect ratio
		m := resize.Thumbnail(size, size, img, resize.NearestNeighbor)

		fd, err = os.Create(cachedPath)
		if err != nil {
			log.Fatal(err)
		}

		// write new image to file
		//buf := bytes.NewBuffer(nil)
		jpeg.Encode(fd, m, nil)
		//imgr := bytes.NewReader(buf.Bytes())
		return
	} else {
		// cached file exists, use it
		fd, err = os.Open(cachedPath)
		return
	}
}
