package main

import (
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	auth "github.com/abbot/go-http-auth"
	"github.com/bradfitz/http2"
	"github.com/gorilla/mux"
	"github.com/nfnt/resize"
	"gopkg.in/yaml.v2"
)

// example configuration file:
// host: example.net
// listen: 0.0.0.0:8064
// certfile: /etc/galilego/server.crt
// keyfile: /etc/galilego/server.key
// users:
//	bob: $1$dlPL2MqE$oQmn16q49SqdmhenQuNgs1
//	alice: $1$dlPL2MqE$oQmn16q49SqdmhenQuNgs1
type configuration struct {
	Host              string
	Listen            string
	CertFile, KeyFile string
	Users             map[string]string
}

var conf configuration

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s - HTTP/2 web gallery written in Go\n"+
			"Usage: %s -c config.yaml\n",
			os.Args[0], os.Args[0])
		flag.PrintDefaults()
	}
	var config = flag.String("c", "config.yaml", "Load configuration from file")
	var makehash = flag.String("makehash", "s3cr3t", "make a password hash")
	flag.Parse()

	if *makehash != "s3cr3t" {
		fmt.Printf("$1$%s\n", auth.MD5Crypt(
			[]byte(*makehash),
			randomBytes(5),
			randomBytes(5)))
		os.Exit(0)
	}

	// load the local configuration file
	fd, err := ioutil.ReadFile(*config)
	if err != nil {
		log.Fatal(err)
	}
	err = yaml.Unmarshal(fd, &conf)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	var srv http.Server
	srv.Addr = conf.Listen

	authenticator := auth.NewBasicAuthenticator(conf.Host, Secret)

	r := mux.NewRouter()

	r.HandleFunc("/", auth.JustCheck(authenticator, home)).Methods("GET")
	r.HandleFunc("/gallery/{galpath:.*}", auth.JustCheck(authenticator, serveGallery)).Methods("GET")

	fs := http.FileServer(http.Dir(`./statics`))
	r.Handle("/statics/{staticfile}", http.StripPrefix("/statics", fs)).Methods("GET")

	http.Handle("/", r)
	http2.ConfigureServer(&srv, &http2.Server{})
	log.Fatal(srv.ListenAndServeTLS(conf.CertFile, conf.KeyFile))
}

func Secret(user, realm string) string {
	if _, ok := conf.Users[user]; ok {
		return conf.Users[user]
	}
	return ""
}
func home(w http.ResponseWriter, r *http.Request) {
	// The "/" pattern matches everything, so we need to check
	// that we're at the root here.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	dirHtml, _ := genGalleryHtml("gallery")
	io.WriteString(w, `<html>
	<head><title>Galilego HTTP/2 web gallery</title>
	<body>
		<h1 style="font-size: 1.5em;">Content of <a href="/">/</a></h1>
`+dirHtml+`
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
		img, imgModTime, err := getImage(galpath, uint(width))
		if err != nil {
			log.Println(err)
		}
		// set expires header to +1 year
		in1year, _ := time.ParseDuration("8760h")
		exp := time.Now().Add(in1year)
		w.Header().Set("Expires", exp.Format(time.RFC1123))
		http.ServeContent(w, r, galpath, imgModTime, img)
		img.Close()
	} else {
		dirHtml, imgHtml := genGalleryHtml(galpath)
		galNav := getGalNav(r.RequestURI)
		io.WriteString(w, `<!DOCTYPE html>
<html>
	<head>
		<meta charset="utf-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<script src="/statics/jquery-2.1.4.min.js"></script>
		<script src="/statics/jssor.slider.mini.js"></script>
		`+jssorParameters+`
		<title>Galilego HTTP/2 web gallery</title>
	</head>
	<body>
	<h1 style="font-size: 1.5em;">Navigation: `+galNav+`</h1>
		<p>Utilisez les fleches pour naviguer. Cliquez sur une image pour telecharger la version originale.</p>
		`+dirHtml+`
		<!-- Jssor Slider Begin -->
		<!-- To move inline styles to css file/block, please specify a class name for each element. --> 
		<div id="slider1_container" style="position: relative; top: 0px; left: 0px; width: 1300px; height: 700px; background: #191919; background-color: white; overflow: hidden;">
			<!-- Loading Screen -->
			<div u="loading" style="position: absolute; top: 0px; left: 0px;">
				<div style="filter: alpha(opacity=70); opacity:0.7; position: absolute; display: block;
					background-color: #000000; top: 0px; left: 0px;width: 100%;height:100%;">
				</div>
				<div style="position: absolute; display: block; background: url(/statics/loading.gif) no-repeat center center;
					top: 0px; left: 0px;width: 100%;height:100%;">
				</div>
			</div>
	
			<!-- Slides Container -->
			<div u="slides" style="cursor: move; position: absolute; left: 130px; top: 0px; width: 1300px; height: 700px; overflow: hidden;">
	   			`+imgHtml+`
			</div>
			`+jssorStyle+`
		</div>
	</body>
</html>`)
	}
}

// genGalleryHtml reads the content of path and returns HTML code that
// represents the gallery
func genGalleryHtml(path string) (dirHtml, imgHtml string) {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("<p>Error: %v</p>", err), ""
	}
	if !fi.Mode().IsDir() {
		return `<p>Error: ` + path + ` is not a valid directory</p>`, ""
	}
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("<p>Error: %v</p>", err), ""
	}
	defer dir.Close()
	dirContent, err := dir.Readdir(-1)
	if err != nil {
		return fmt.Sprintf("<p>Error: %v</p>", err), ""
	}
	for _, dirEntry := range dirContent {
		if dirEntry.IsDir() {
			// if the entry is a folder, add a folder icon
			dirHtml += fmt.Sprintf("<div><a href=\"/%s/%s\"><img src=\"/statics/f.jpg\" alt=\"%s\"/>%s</a></div>",
				path, dirEntry.Name(), dirEntry.Name(), dirEntry.Name())
		} else if dirEntry.Mode().IsRegular() && imgre.MatchString(dirEntry.Name()) {
			// if the entry is an image, display its miniature
			imgHtml += fmt.Sprintf(`<div>
	<a href="/%s/%s"><img u="image" src="/%s/%s?width=1200" /></a>
	<img u="thumb" src="/%s/%s?width=300" />
</div>
`, path, dirEntry.Name(), path, dirEntry.Name(), path, dirEntry.Name())
		}
	}
	return
}

func getImage(path string, size uint) (fd *os.File, modtime time.Time, err error) {
	var fi os.FileInfo
	if size == 0 {
		// if size is zero, serve the file directly
		fd, err = os.Open(path)
		if err != nil {
			return
		}
		fi, err = os.Stat(path)
		if err != nil {
			return
		}
		modtime = fi.ModTime()
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
		jpeg.Encode(fd, m, nil)
		modtime = time.Now()
		return
	} else {
		// cached file exists, use it
		fd, err = os.Open(cachedPath)
		if err != nil {
			return
		}
		fi, err = os.Stat(cachedPath)
		if err != nil {
			return
		}
		modtime = fi.ModTime()
		return
	}
}

func getGalNav(reqPath string) (galNav string) {
	comps := strings.Split(reqPath, "/")
	var prefix string
	for _, comp := range comps {
		if comp == "" {
			continue
		}
		galNav += fmt.Sprintf(`/&nbsp;<a href="%s/%s/">%s</a>&nbsp;`, prefix, comp, comp)
		prefix += "/" + comp
	}
	return
}

var jssorParameters string = `
	<script>
		jQuery(document).ready(function ($) {
			var _SlideshowTransitions = [
				{$Duration: 400, x: 0.3, $During: { $Left: [0.3, 0.7] }, $Easing: { $Left: $JssorEasing$.$EaseInCubic, $Opacity: $JssorEasing$.$EaseLinear }, $Opacity: 2 }
			];
			var options = {
				$FillMode: 1,                                   //[Optional] The way to fill image in slide, 0 stretch, 1 contain (keep aspect ratio and put all inside slide), 2 cover (keep aspect ratio and cover whole slide), 4 actual size, 5 contain for large image, actual size for small image, default value is 0
				$Loop: 2,					//[Optional] Enable loop(circular) of carousel or not, 0: stop, 1: loop, 2 rewind, default value is 1
				$AutoPlay: true,				//[Optional] Whether to auto play, to enable slideshow, this option must be set to true, default value is false
				$AutoPlayInterval: 3000,			//[Optional] Interval (in milliseconds) to go for next slide since the previous stopped if the slider is auto playing, default value is 3000
				$PauseOnHover: 1,				//[Optional] Whether to pause when mouse over if a slider is auto playing, 0 no pause, 1 pause for desktop, 2 pause for touch device, 3 pause for desktop and touch device, 4 freeze for desktop, 8 freeze for touch device, 12 freeze for desktop and touch device, default value is 1
				$DragOrientation: 3,				//[Optional] Orientation to drag slide, 0 no drag, 1 horizental, 2 vertical, 3 either, default value is 1 (Note that the $DragOrientation should be the same as $PlayOrientation when $DisplayPieces is greater than 1, or parking position is not 0)
				$ArrowKeyNavigation: true,   			//[Optional] Allows keyboard (arrow key) navigation or not, default value is false
				$SlideDuration: 1,				//Specifies default duration (swipe) for slide in milliseconds
				$SlideshowOptions: {				//[Optional] Options to specify and enable slideshow or not
					$Class: $JssorSlideshowRunner$,		//[Required] Class to create instance of slideshow
					$Transitions: _SlideshowTransitions,	//[Required] An array of slideshow transitions to play slideshow
					$TransitionsOrder: 1,			//[Optional] The way to choose transition to play slide, 1 Sequence, 0 Random
					$ShowLink: true				//[Optional] Whether to bring slide link on top of the slider when slideshow is running, default value is false
				},
				$ArrowNavigatorOptions: {			//[Optional] Options to specify and enable arrow navigator or not
					$Class: $JssorArrowNavigator$,		//[Requried] Class to create arrow navigator instance
					$ChanceToShow: 1,			//[Required] 0 Never, 1 Mouse Over, 2 Always
					$AutoCenter: 2,				//[Optional] Auto center navigator in parent container, 0 None, 1 Horizontal, 2 Vertical, 3 Both, default value is 0
					$Steps: 1				//[Optional] Steps to go for each navigation request, default value is 1
				},
				$ThumbnailNavigatorOptions: {			//[Optional] Options to specify and enable thumbnail navigator or not
					$Class: $JssorThumbnailNavigator$,	//[Required] Class to create thumbnail navigator instance
					$ChanceToShow: 2,			//[Required] 0 Never, 1 Mouse Over, 2 Always
					$Scale: true,
					$ActionMode: 1,				//[Optional] 0 None, 1 act by click, 2 act by mouse hover, 3 both, default value is 1
					$Lanes: 2,				//[Optional] Specify lanes to arrange thumbnails, default value is 1
					$SpacingX: 10,				//[Optional] Horizontal space between each thumbnail in pixel, default value is 0
					$SpacingY: 10,				//[Optional] Vertical space between each thumbnail in pixel, default value is 0
					$DisplayPieces: 10,			//[Optional] Number of pieces to display, default value is 1
					$ParkingPosition: 50,			//[Optional] The offset position to park thumbnail
					$Orientation: 2				//[Optional] Orientation to arrange thumbnails, 1 horizental, 2 vertical, default value is 1
				}
			};
			var jssor_slider1 = new $JssorSlider$("slider1_container", options);

			//responsive code begin
			//you can remove responsive code if you don't want the slider scales
			//while window resizes
			function ScaleSlider() {
				var parentWidth = $('#slider1_container').parent().width();
				if (parentWidth) {
					jssor_slider1.$ScaleWidth(parentWidth);
				}
				else
					window.setTimeout(ScaleSlider, 30);
			}
			//Scale slider after document ready
			ScaleSlider();
											
			//Scale slider while window load/resize/orientationchange.
			$(window).bind("load", ScaleSlider);
			$(window).bind("resize", ScaleSlider);
			$(window).bind("orientationchange", ScaleSlider);
			//responsive code end

			var parentHeight = jssor_slider1.$Elmt.parentNode.clientHeight;
			if (parentHeight) {
				var sliderOriginalWidth = jssor_slider1.$OriginalWidth();
				var sliderOriginalHeight = jssor_slider1.$OriginalHeight();
				var newWidthToFitParentContainer = parentHeight / sliderOriginalHeight * sliderOriginalWidth;
				if (newWidthToFitParentContainer > jssor_slider1.$Elmt.parentNode.clientWidth) {
					//scale differently if the width of the slider is greater than the parent
					jssor_slider1.$ScaleWidth(jssor_slider1.$Elmt.parentNode.clientWidth-30);
				} else {
					jssor_slider1.$ScaleWidth(newWidthToFitParentContainer);
				}
			} else {
				window.setTimeout(ScaleSlider, 30);
			}
		});
	</script>
`

var jssorStyle string = `
		<script>jssor_slider1_starter('slider1_container');</script>
		<!--#region Arrow Navigator Skin Begin -->
		<style>
			/* jssor slider arrow navigator skin 05 css */
			/*
			.jssora05l				  (normal)
			.jssora05r				  (normal)
			.jssora05l:hover			(normal mouseover)
			.jssora05r:hover			(normal mouseover)
			.jssora05l.jssora05ldn	  (mousedown)
			.jssora05r.jssora05rdn	  (mousedown)
			*/
			.jssora05l, .jssora05r {
				display: block;
				position: absolute;
				/* size of arrow element */
				width: 40px;
				height: 40px;
				cursor: pointer;
				background: url(/statics/a17.png) no-repeat;
				overflow: hidden;
			}
			.jssora05l { background-position: -10px -40px; }
			.jssora05r { background-position: -70px -40px; }
			.jssora05l:hover { background-position: -130px -40px; }
			.jssora05r:hover { background-position: -190px -40px; }
			.jssora05l.jssora05ldn { background-position: -250px -40px; }
			.jssora05r.jssora05rdn { background-position: -310px -40px; }
		</style>
		<!-- Arrow Left -->
		<span u="arrowleft" class="jssora05l" style="top: 158px; left: 248px;">
		</span>
		<!-- Arrow Right -->
		<span u="arrowright" class="jssora05r" style="top: 158px; right: 8px">
		</span>
		<!--#endregion Arrow Navigator Skin End -->
		<!--#region Thumbnail Navigator Skin Begin -->
		<!-- Help: http://www.jssor.com/development/slider-with-thumbnail-navigator-jquery.html -->
		<style>
			/* jssor slider thumbnail navigator skin 02 css */
			/*
			.jssort02 .p			(normal)
			.jssort02 .p:hover	  (normal mouseover)
			.jssort02 .p.pav		(active)
			.jssort02 .p.pdn		(mousedown)
			*/

			.jssort02 {
				position: absolute;
				/* size of thumbnail navigator container */
				width: 280px;
				height: 100%;
			}

			.jssort02 .p {
				position: absolute;
				top: 0;
				left: 0;
				width: 99px;
				height: 66px;
			}

			.jssort02 .t {
				position: absolute;
				top: 0;
				left: 0;
				width: 100%;
				height: 100%;
				border: none;
			}

			.jssort02 .w {
				position: absolute;
				top: 0px;
				left: 0px;
				width: 100%;
				height: 100%;
			}

			.jssort02 .c {
				position: absolute;
				top: 0px;
				left: 0px;
				width: 95px;
				height: 62px;
				border: #000 2px solid;
				box-sizing: content-box;
				background: url(/statics/t01.png) -800px -800px no-repeat;
				_background: none;
			}

			.jssort02 .pav .c {
				top: 2px;
				_top: 0px;
				left: 2px;
				_left: 0px;
				width: 95px;
				height: 62px;
				border: #000 0px solid;
				_border: #fff 2px solid;
				background-position: 50% 50%;
			}

			.jssort02 .p:hover .c {
				top: 0px;
				left: 0px;
				width: 97px;
				height: 64px;
				border: #fff 1px solid;
				background-position: 50% 50%;
			}

			.jssort02 .p.pdn .c {
				background-position: 50% 50%;
				width: 95px;
				height: 62px;
				border: #000 2px solid;
			}

			* html .jssort02 .c, * html .jssort02 .pdn .c, * html .jssort02 .pav .c {
				/* ie quirks mode adjust */
				width /**/: 99px;
				height /**/: 66px;
			}
		</style>

		<!-- thumbnail navigator container -->
		<div u="thumbnavigator" class="jssort02" style="left: 0px; bottom: 0px;">
			<!-- Thumbnail Item Skin Begin -->
			<div u="slides" style="cursor: default;">
				<div u="prototype" class="p">
					<div class=w><div u="thumbnailtemplate" class="t"></div></div>
					<div class=c></div>
				</div>
			</div>
			<!-- Thumbnail Item Skin End -->
		</div>
		<!--#endregion Thumbnail Navigator Skin End -->
`

func randomBytes(l int) []byte {
	bytes := make([]byte, l)
	for i := 0; i < l; i++ {
		bytes[i] = byte(randInt(65, 90))
	}
	return bytes
}

func randInt(min int, max int) int {
	return min + rand.Intn(max-min)
}
