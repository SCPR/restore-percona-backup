package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/cheggaaa/pb"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type BackupRun struct {
	// authenticated s3 download URL
	RunType      string
	DownloadURL  string
	ExtractedDir string
}

type RestoreJSON struct {
	Base         string
	CreatedAt    time.Time
	Databases    string
	Incrementals []string
}

type Restore struct {
	JSON      *RestoreJSON
	download  chan *BackupRun
	apply     chan *BackupRun
	TargetDir string
}

func NewRestore(rj *RestoreJSON) *Restore {
	return &Restore{
		JSON:     rj,
		download: make(chan *BackupRun, 100),
		apply:    make(chan *BackupRun, 100),
	}
}

func (r *Restore) Run() error {
	// iterate through the base and each incremental, running through a pipeline
	// of Fetch -> Extract -> Apply
	var wg sync.WaitGroup

	go r.downloadRuns()
	go r.applyRuns(&wg)

	log.Printf("Adding %d to WaitGroup", 1+len(r.JSON.Incrementals))
	wg.Add(1 + len(r.JSON.Incrementals))

	r.download <- &BackupRun{
		RunType:     "full",
		DownloadURL: r.JSON.Base,
	}

	for _, inc := range r.JSON.Incrementals {
		r.download <- &BackupRun{
			RunType:     "incremental",
			DownloadURL: inc,
		}
	}

	close(r.download)

	wg.Wait()

	// call prepare one last time
	cmd := exec.Command("innobackupex", "--apply-log", r.TargetDir)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		log.Fatal("Failed to run final prepare: ", err)
	}

	log.Printf("Backup is prepared in target dir of %s", r.TargetDir)

	// now move the backup into place
	rsync_cmd := exec.Command("rsync", "-rvt", "--exclude", "'xtrabackup_checkpoints'", "--exclude", "'xtrabackup_logfile'", fmt.Sprintf("%s/", r.TargetDir), "/var/lib/mysql/")
	rsync_cmd.Stdout = os.Stdout
	rsync_cmd.Stderr = os.Stderr

	err = rsync_cmd.Run()
	if err != nil {
		log.Fatal("Failed to rsync files to /var/lib/mysql/: ", err)
	}

	log.Printf("Backup is moved into /var/lib/mysql")

	return nil
}

func (r *Restore) downloadRuns() {
	defer close(r.apply)

	for br := range r.download {
		log.Printf("Downloading %s", br.DownloadURL)

		// create a tempdir
		dir, err := ioutil.TempDir("", "backup")

		if err != nil {
			log.Fatal("Failed to create TempDir: ", err)
		}

		log.Printf("Writing to tempdir of %s", dir)
		br.ExtractedDir = dir

		// read our backuprun into the tempdir, via gzip | xbstream
		cmd := exec.Command("xbstream", "-x", "-C", dir)

		resp, err := http.Get(br.DownloadURL)

		if err != nil {
			log.Fatal("Failed to fetch backup run: ", err)
		}

		defer resp.Body.Close()

		progress := pb.New(int(resp.ContentLength)).SetUnits(pb.U_BYTES)
		progress.SetWidth(80)
		progress.Start()

		pbreader := progress.NewProxyReader(resp.Body)

		reader, err := gzip.NewReader(pbreader)

		if err != nil {
			log.Fatal("Failed to set up gzip reader: ", err)
		}

		cmd.Stdin = reader
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			log.Fatal("xbstream failed: ", err)
		}

		r.apply <- br
	}
}

func (r *Restore) applyRuns(wg *sync.WaitGroup) {
	for br := range r.apply {
		log.Printf("Applying backup from %s", br.ExtractedDir)

		if r.TargetDir == "" {
			if br.RunType != "full" {
				log.Fatal("Fatal: Target dir is not set, but first run is not incremental.")
			}

			log.Printf("Setting target dir to %s", br.ExtractedDir)
			r.TargetDir = br.ExtractedDir
		}

		args := []string{"--apply-log", "--redo-only", r.TargetDir}

		if br.RunType == "incremental" {
			args = append(args, "--incremental-dir", br.ExtractedDir)
		}

		log.Printf("Running innobackupex %s", strings.Join(args, " "))
		cmd := exec.Command("innobackupex", args...)

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		cmd.Run()

		wg.Done()
	}

	log.Printf("Done applying")
}

func main() {
	var (
		uri = flag.String("uri", "http://ops-deploybot.scprdev.org/backups/restore_json", "URI to download restore JSON")
	)
	flag.Parse()
	token := flag.Arg(0)

	restore_json_url := fmt.Sprintf("%s?token=%s", *uri, token)

	// Fetch the restore json
	resp, err := http.Get(restore_json_url)

	if err != nil {
		log.Fatal("Failed to fetch restore JSON: ", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Fatal("Non-200 response from attempt to read restore JSON: ", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Failed to read restore JSON: ", err)
	}

	var restoreJSON RestoreJSON
	err = json.Unmarshal(body, &restoreJSON)

	restore := NewRestore(&restoreJSON)

	restore.Run()

}
