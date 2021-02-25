package api

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
	"github.com/h2non/filetype"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type DBusApi struct {
	Api
	daemon     *exec.Cmd
	webhookDir string
	webhookList []string
}

func NewDBusApi(signalCliConfig string, attachmentTmpDir string, avatarTmpDir string) *DBusApi {
	return &DBusApi {
		Api: Api{
			signalCliConfig:  signalCliConfig,
			attachmentTmpDir: attachmentTmpDir,
			avatarTmpDir:     avatarTmpDir,
		},
		webhookDir: filepath.Join(signalCliConfig, "webhook"),
	}
}

func (a *DBusApi) AddWebhook(url string) error {
	raw := []byte(url)
	hash := md5.Sum(raw)
	filename := fmt.Sprintf("%x", hash)
	
	if _,err := os.Stat(a.webhookDir); err != nil && os.IsNotExist(err) {
		os.MkdirAll(a.webhookDir, os.ModePerm)
	}
	
	return ioutil.WriteFile(filepath.Join(a.webhookDir, filename), raw, os.ModePerm)
}

func (a *DBusApi) RemoveWebhook(url string) error {
	hash := md5.Sum([]byte(url))
	filename := fmt.Sprintf("%x", hash)
	fullpath := filepath.Join(a.webhookDir, filename)
	
	return os.RemoveAll(fullpath)
}

func (a *DBusApi) getWebHookList() {
	list := []string{}
	
	filepath.Walk(a.webhookDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		
		fileRaw, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}
		
		url := string(fileRaw);
		list = append(list, url)
		
		return nil
	})
	
	a.webhookList = list
}

// Daemon starts the dbus daemon and receives forever.
func (a *DBusApi) Daemon() error {
	cmd := exec.Command("signal-cli", "--config", a.signalCliConfig, "--output=json", "daemon", "--system")
	
	outReader, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	err = cmd.Start()
	if err != nil {
		log.Error(err)
		return err
	}
	a.daemon = cmd
	
	scanner := bufio.NewScanner(outReader)
	log.Infof("scanning stdout")
	for scanner.Scan() {
		wire := scanner.Bytes()
		log.Debugf("wire (length %d): %s", len(wire), wire)
		if json.Valid(wire) {
			a.FetchWebHook(bytes.NewBuffer(wire))
		}
	}
	return nil
}

func (a *DBusApi) StopDaemon() error {
	if a.daemon != nil && a.daemon.Process != nil {
		return a.daemon.Process.Kill()
	}
	
	return nil
}

func (a *DBusApi) FetchWebHook(raw *bytes.Buffer) {
	httpClient := http.Client{
		Timeout: time.Second * 30,
	}
	
	for _, url := range a.webhookList {
		go func() {
			_, err := httpClient.Post(url, "application/json", raw)
			if err != nil {
				log.Error(err)
			}
		}()
	}
}

// SendDbus does the same thing as Send but it goes through a running daemon.
func (a *DBusApi) send(c *gin.Context, attachmentTmpDir string, signalCliConfig string, number string, message string,
	recipients []string, base64Attachments []string, isGroup bool) {
	cmd := []string{"--dbus-system", "send"}
	
	if len(recipients) == 0 {
		c.JSON(400, gin.H{"error": "Please specify at least one recipient"})
		return
	}
	
	if !isGroup {
		cmd = append(cmd, recipients...)
	} else {
		if len(recipients) > 1 {
			c.JSON(400, gin.H{"error": "More than one recipient is currently not allowed"})
			return
		}
		
		groupId, err := base64.StdEncoding.DecodeString(recipients[0])
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid group id"})
			return
		}
		
		cmd = append(cmd, []string{"-g", string(groupId)}...)
	}
	
	attachmentTmpPaths := []string{}
	for _, base64Attachment := range base64Attachments {
		u, err := uuid.NewV4()
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		
		dec, err := base64.StdEncoding.DecodeString(base64Attachment)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		
		fType, err := filetype.Get(dec)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		
		attachmentTmpPath := attachmentTmpDir + u.String() + "." + fType.Extension
		attachmentTmpPaths = append(attachmentTmpPaths, attachmentTmpPath)
		
		f, err := os.Create(attachmentTmpPath)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		defer f.Close()
		
		if _, err := f.Write(dec); err != nil {
			cleanupTmpFiles(attachmentTmpPaths)
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if err := f.Sync(); err != nil {
			cleanupTmpFiles(attachmentTmpPaths)
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		
		f.Close()
	}
	
	if len(attachmentTmpPaths) > 0 {
		cmd = append(cmd, "-a")
		cmd = append(cmd, attachmentTmpPaths...)
	}
	
	_, err := runSignalCli(true, cmd, message)
	if err != nil {
		cleanupTmpFiles(attachmentTmpPaths)
		if strings.Contains(err.Error(), signalCliV2GroupError) {
			c.JSON(400, Error{Msg: "Cannot send message to group - please first update your profile."})
		} else {
			c.JSON(400, Error{Msg: err.Error()})
		}
		return
	}
	
	cleanupTmpFiles(attachmentTmpPaths)
	c.JSON(201, nil)
}