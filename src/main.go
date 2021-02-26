package main

import (
	"flag"
	"os"
	"path/filepath"
	
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"signal-cli-rest-api/api"
	_ "signal-cli-rest-api/docs"

)



// @title Signal Cli REST API
// @version 1.0
// @description This is the Signal Cli REST API documentation.

// @tag.name General
// @tag.description Some general endpoints.

// @tag.name Devices
// @tag.description Register and link Devices.

// @tag.name Groups
// @tag.description Create, List and Delete Signal Groups.

// @tag.name Messages
// @tag.description Send and Receive Signal Messages.

// @tag.name Attachments 
// @tag.description List and Delete Attachments.

// @tag.name Profiles 
// @tag.description Update Profile.

// @tag.name Identities
// @tag.description List and Trust Identities.

// @host 127.0.0.1:8080
// @BasePath /
func main() {
	signalCliConfig := flag.String("signal-cli-config", "/home/.local/share/signal-cli/", "Config directory where signal-cli config is stored")
	attachmentTmpDir := flag.String("attachment-tmp-dir", "/tmp/", "Attachment tmp directory")
	avatarTmpDir := flag.String("avatar-tmp-dir", "/tmp/", "Avatar tmp directory")
	uiDir := flag.String("web-ui-dir", filepath.Dir(os.Args[0]), "Web UI Root")
	flag.Parse()

	router := gin.New()
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/v1/health"}, //do not log the health requests (to avoid spamming the log file)
	}))

	router.Use(gin.Recovery())

	log.Info("Started Signal Messenger REST API")

	apiInstance := api.NewDBusApi(*signalCliConfig, *attachmentTmpDir, *avatarTmpDir)
	v1 := router.Group("/v1")
	{
		about := v1.Group("/about")
		{
			about.GET("", apiInstance.About)
		}

		configuration := v1.Group("/configuration")
		{
			configuration.GET("", apiInstance.GetConfiguration)
			configuration.POST("", apiInstance.SetConfiguration)
		}

		health := v1.Group("/health")
		{
			health.GET("", apiInstance.Health)
		}

		register := v1.Group("/register")
		{
			register.POST(":number", apiInstance.RegisterNumber)
			register.POST(":number/verify/:token", apiInstance.VerifyRegisteredNumber)
		}

		sendV1 := v1.Group("/send")
		{
			sendV1.POST("", apiInstance.Send)
		}

		receive := v1.Group("/receive")
		{
			receive.GET(":number", apiInstance.Receive)
		}

		groups := v1.Group("/groups")
		{
			groups.POST(":number", apiInstance.CreateGroup)
			groups.GET(":number", apiInstance.GetGroups)
			groups.GET(":number/:groupid", apiInstance.GetGroup)
			groups.DELETE(":number/:groupid", apiInstance.DeleteGroup)
		}

		link := v1.Group("qrcodelink")
		{
			link.GET("", apiInstance.GetQrCodeLink)
		}

		attachments := v1.Group("attachments")
		{
			attachments.GET("", apiInstance.GetAttachments)
			attachments.DELETE(":attachment", apiInstance.RemoveAttachment)
			attachments.GET(":attachment", apiInstance.ServeAttachment)
		}

		profiles := v1.Group("profiles")
		{
			profiles.PUT(":number", apiInstance.UpdateProfile)
		}

		identities := v1.Group("identities")
		{
			identities.GET(":number", apiInstance.ListIdentities)
			identities.PUT(":number/trust/:numbertotrust", apiInstance.TrustIdentity)
		}
	}

	v2 := router.Group("/v2")
	{
		sendV2 := v2.Group("/send")
		{
			sendV2.POST("", apiInstance.SendV2)
		}
	}
	
	v3 := router.Group("/v3")
	{
		webhookGroup := v3.Group("/webhook")
		{
			webhookGroup.GET("", apiInstance.ApiListWebhook)
			webhookGroup.POST("", apiInstance.ApiAddWebhook)
			webhookGroup.POST("/remove", apiInstance.ApiRemoveWebhook)
		}
	}

	swaggerUrl := ginSwagger.URL("/swagger/doc.json")
	webrootPath, err := filepath.Abs(*uiDir);
	if err == nil {
		router.Static("/ui", webrootPath)
	}
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, swaggerUrl))

	go func() {
		err := apiInstance.Daemon()
		if err != nil {
			panic("daemon is down")
		}
	}()
	
	router.Run()
}

func getEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}
