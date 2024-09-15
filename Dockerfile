FROM golang:1.23.1-alpine

WORKDIR /app

COPY go.* ./

RUN go install github.com/air-verse/air@latest

RUN go get github.com/markbates/goth/gothic 

RUN go get github.com/gorilla/sessions

RUN go mod download

COPY . .

EXPOSE 8000

CMD [ "air", "-c", "air.toml" ]
