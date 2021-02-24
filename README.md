# Dockerized Signal Messenger REST API

[![dockeri.co](https://dockeri.co/image/mmhk/signal-cli-rest-api)](https://hub.docker.com/r/mmhk/signal-cli-rest-api)

This project creates a small dockerized REST API around [signal-cli](https://github.com/AsamK/signal-cli).

At the moment, the following functionality is exposed via REST:

- Register a number
- Verify the number using the code received via SMS
- Send message (+ attachments) to multiple recipients (or a group)
- Receive messages
- Link devices
- Create/List/Remove groups
- List/Serve/Delete attachments
- Update profile

## Examples

Sample `docker-compose.yml`file:

```sh
version: "3"
services:
  signal-cli-rest-api:
    image: mmhk/signal-cli-rest-api:latest
    ports:
      - "8080:8080" #map docker port 8080 to host port 8080.
    volumes:
      - "./signal-cli-config:/home/.local/share/signal-cli" #map "signal-cli-config" folder on host system into docker container. the folder contains the password and cryptographic keys when a new number is registered

```

The Swagger API documentation can be found [here](https://bbernhard.github.io/signal-cli-rest-api/). If you prefer a simple text file like API documentation have a look [here](https://github.com/bbernhard/signal-cli-rest-api/blob/master/doc/EXAMPLES.md)

In case you need more functionality, please **file a ticket** or **create a PR**.

### 使用帮助

- 进入项目目录
- 执行 `docker-compose up -d`
- 打开 http://127.0.0.1:8080/ui/index.html
- 执行 `/v1/qrcodelink`， 设置一个设备名称。生成二维码，使用Signal APP 绑定设备
- 执行 `/v1/receive/{number}` , `timeout` 设置 10 （秒）, 接收所有的上行消息
