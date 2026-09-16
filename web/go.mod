// 该文件仅用于让 Go 工具链在本目录停止遍历:
// web/node_modules 中可能包含第三方 Go 源码(如 flatted/golang),
// 声明为独立模块后 go build/vet/test ./... 不会再把它们纳入。
module icloud-hme/web

go 1.26
