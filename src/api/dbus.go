package api

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
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

type WebHookRequest struct {
	URL string `json:"url"`
}

type ServiceResult struct {
	Status bool        `json:"status"`
	Data   interface{} `json:"data"`
	Error  error       `json:"error"`
}

func NewDBusApi(signalCliConfig string, attachmentTmpDir string, avatarTmpDir string) *DBusApi {
	return &DBusApi {
		Api: Api{
			signalCliConfig:  signalCliConfig,
			attachmentTmpDir: attachmentTmpDir,
			avatarTmpDir:     avatarTmpDir,
		},
		webhookDir: filepath.Join(signalCliConfig, "webhook"),
		webhookList: []string{},
	}
}

func (a *DBusApi) AddWebhook(url string) error {
	raw := []byte(url)
	hash := md5.Sum(raw)
	filename := fmt.Sprintf("%x", hash)
	
	if _,err := os.Stat(a.webhookDir); err != nil && os.IsNotExist(err) {
		os.MkdirAll(a.webhookDir, os.ModePerm)
	}
	
	err := ioutil.WriteFile(filepath.Join(a.webhookDir, filename), raw, os.ModePerm)
	
	a.getWebHookList()
	
	return err
}

func (a *DBusApi) RemoveWebhook(url string) error {
	hash := md5.Sum([]byte(url))
	filename := fmt.Sprintf("%x", hash)
	fullpath := filepath.Join(a.webhookDir, filename)
	
	err := os.RemoveAll(fullpath)
	
	a.getWebHookList()
	
	return err
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
	
	a.getWebHookList()
	
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
	
	err = cmd.Wait()
	if err != nil {
		log.Error(err)
		return err
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
	
	for _, url := range a.webhookList {
		buf := bytes.NewBuffer(raw.Bytes())
		go func(url string, raw *bytes.Buffer) {
			httpClient := http.Client{
				Timeout: time.Second * 30,
			}
			_, err := httpClient.Post(url, "application/json", raw)
			if err != nil {
				log.Error(err)
			}
		}(url, buf)
	}
}

// SendDbus does the same thing as Send but it goes through a running daemon.
func (a *DBusApi) send(c *gin.Context, attachmentTmpDir string, signalCliConfig string, number string, message string,
	recipients []string, base64Attachments []string, isGroup bool) {
	cmd := []string{"--dbus-system", "--username", number, "send"}
	
	if len(recipients) == 0 {
		c.JSON(500, ServiceResult{
			Status: false,
			Error: errors.New("Please specify at least one recipient"),
		})
		return
	}
	
	if !isGroup {
		cmd = append(cmd, recipients...)
	} else {
		if len(recipients) > 1 {
			c.JSON(500, ServiceResult{
				Status: false,
				Error: errors.New("More than one recipient is currently not allowed"),
			})
			return
		}
		
		groupId, err := base64.StdEncoding.DecodeString(recipients[0])
		if err != nil {
			c.JSON(500, ServiceResult{
				Status: false,
				Error: errors.New("Invalid group id"),
			})
			return
		}
		
		cmd = append(cmd, []string{"-g", string(groupId)}...)
	}
	
	attachmentTmpPaths := []string{}
	for _, base64Attachment := range base64Attachments {
		u, err := uuid.NewV4()
		if err != nil {
			c.JSON(500, ServiceResult{
				Status: false,
				Error: err,
			})
			return
		}
		
		dec, err := base64.StdEncoding.DecodeString(base64Attachment)
		if err != nil {
			c.JSON(500, ServiceResult{
				Status: false,
				Error: err,
			})
			return
		}
		
		fType, err := filetype.Get(dec)
		if err != nil {
			c.JSON(500, ServiceResult{
				Status: false,
				Error: err,
			})
			return
		}
		
		attachmentTmpPath := attachmentTmpDir + u.String() + "." + fType.Extension
		attachmentTmpPaths = append(attachmentTmpPaths, attachmentTmpPath)
		
		f, err := os.Create(attachmentTmpPath)
		if err != nil {
			c.JSON(500, ServiceResult{
				Status: false,
				Error: err,
			})
			return
		}
		defer f.Close()
		
		if _, err := f.Write(dec); err != nil {
			cleanupTmpFiles(attachmentTmpPaths)
			c.JSON(500, ServiceResult{
				Status: false,
				Error: err,
			})
			return
		}
		if err := f.Sync(); err != nil {
			cleanupTmpFiles(attachmentTmpPaths)
			c.JSON(500, ServiceResult{
				Status: false,
				Error: err,
			})
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
			c.JSON(500, ServiceResult{
				Status: false,
				Error: errors.New("Cannot send message to group - please first update your profile."),
			})
		} else {
			c.JSON(500, ServiceResult{
				Status: false,
				Error: err,
			})
		}
		return
	}
	
	cleanupTmpFiles(attachmentTmpPaths)
	c.JSON(200, ServiceResult{
		Status: true,
	})
}


// @Summary List all Receive Signal Messages callback URL.
// @Tags WebHook
// @Description List all Receive Signal Messages callback URL.
// @Accept  json
// @Produce  json
// @Success 200 {object} []string
// @Failure 400 {object} Error
// @Router /v3/webhook [get]
func (a *DBusApi) ApiListWebhook(c *gin.Context) {
	c.JSON(200, ServiceResult{
		Status: true,
		Data: a.webhookList,
	})
}


// @Summary Add a Receive Signal Messages callback URL.
// @Tags WebHook
// @Description Add a Receive Signal Messages callback URL.
// @Accept  json
// @Produce  json
// @Success 200 {object} ServiceResult{data=string}
// @Failure 500 {object} ServiceResult{data=string}
// @Param data body WebHookRequest true "WebHookRequest"
// @Router /v3/webhook [post]
func (a *DBusApi) ApiAddWebhook(c *gin.Context) {
	var req WebHookRequest
	err := c.BindJSON(&req)
	if err != nil {
		c.JSON(500, ServiceResult{
			Status:false,
			Error: err,
		})
		log.Error(err.Error())
		return
	}
	
	if len(req.URL) == 0 {
		c.JSON(500, ServiceResult{
			Status:false,
			Error: errors.New(`URL can't been empty`),
		})
		return
	}
	
	err = a.AddWebhook(req.URL)
	if err != nil {
		c.JSON(500, ServiceResult{
			Status:false,
			Error: err,
		})
		log.Error(err.Error())
		return
	}
	
	c.JSON(200, ServiceResult{
		Status: true,
		Data: "OK",
	})
}

// @Summary Add a Receive Signal Messages callback URL.
// @Tags WebHook
// @Description Add a Receive Signal Messages callback URL.
// @Accept  json
// @Produce  json
// @Success 200 {object} ServiceResult{data=string}
// @Failure 500 {object} ServiceResult{data=string}
// @Param data body WebHookRequest true "WebHookRequest"
// @Router /v3/webhook/remove [post]
func (a *DBusApi) ApiRemoveWebhook(c *gin.Context) {
	var req WebHookRequest
	err := c.BindJSON(&req)
	if err != nil {
		c.JSON(500, ServiceResult{
			Status:false,
			Error: err,
		})
		log.Error(err.Error())
		return
	}
	
	if len(req.URL) == 0 {
		c.JSON(500, ServiceResult{
			Status:false,
			Error: errors.New(`URL can't been empty`),
		})
		return
	}
	
	err = a.RemoveWebhook(req.URL)
	if err != nil {
		c.JSON(500, ServiceResult{
			Status:false,
			Error: err,
		})
		log.Error(err.Error())
		return
	}
	
	c.JSON(200, ServiceResult{
		Status: true,
		Data: "OK",
	})
}

// @Summary Send a signal message.
// @Tags Messages
// @Description Send a signal message
// @Accept  json
// @Produce  json
// @Success 200 {object} ServiceResult{data=string}
// @Failure 500 {object} ServiceResult{data=string}
// @Param data body SendMessageV2 true "Input Data"
// @Router /v2/send [post]
func (a *DBusApi) SendV2(c *gin.Context) {
	var req SendMessageV2
	err := c.BindJSON(&req)
	if err != nil {
		c.JSON(500, ServiceResult{
			Status: false,
			Error: errors.New("Couldn't process request - invalid request"),
		})
		log.Error(err.Error())
		return
	}
	
	if len(req.Recipients) == 0 {
		c.JSON(500, ServiceResult{
			Status: false,
			Error: errors.New("Couldn't process request - please provide at least one recipient"),
		})
		return
	}
	
	groups := []string{}
	recipients := []string{}
	
	for _, recipient := range req.Recipients {
		if strings.HasPrefix(recipient, groupPrefix) {
			groups = append(groups, strings.TrimPrefix(recipient, groupPrefix))
		} else {
			recipients = append(recipients, recipient)
		}
	}
	
	if len(recipients) > 0 && len(groups) > 0 {
		c.JSON(500, ServiceResult{
			Status: false,
			Error: errors.New("Signal Messenger Groups and phone numbers cannot be specified together in one request! Please split them up into multiple REST API calls."),
		})
		return
	}
	
	if len(groups) > 1 {
		c.JSON(500, ServiceResult{
			Status: false,
			Error: errors.New("A signal message cannot be sent to more than one group at once! Please use multiple REST API calls for that."),
		})
		return
	}
	
	for _, group := range groups {
		a.send(c, a.attachmentTmpDir, a.signalCliConfig, req.Number, req.Message, []string{group}, req.Base64Attachments, true)
	}
	
	if len(recipients) > 0 {
		a.send(c, a.attachmentTmpDir, a.signalCliConfig, req.Number, req.Message, recipients, req.Base64Attachments, false)
	}
}