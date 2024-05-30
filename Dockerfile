# 使用官方的 Go 基础镜像，这里使用的是 1.19 版本，您可以根据需要选择合适的版本
FROM golang:1.19 as builder

# 设置工作目录
WORKDIR /app

# 复制 go.mod 和 go.sum 文件
COPY go.mod ./
COPY go.sum ./

# 下载依赖
RUN go mod download

# 复制整个项目到容器中
COPY . .

# 编译项目，这里假设主要的执行文件在 main.go
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# 使用 scratch 作为最小运行环境
FROM scratch

# 从 builder 镜像中复制编译好的执行文件
COPY --from=builder /app/main .

# 声明服务运行在 8080 端口
EXPOSE 8080

# 运行应用
CMD ["./main"]
