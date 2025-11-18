# 说明文档

## 心跳检查
```
curl http://127.0.0.1:8080/health
```


## 注册主机，使用ansible初始化主机，在redis中注册主机名锁
```
curl -N -s http://127.0.0.1:8080/v1/host/register \
-H 'Content-Type: application/json' \
-d '{"ID":"biz-goods","Hostname":"prod-goods-ms-001","IP":"10.1.2.3"}'
```


## 注销主机，在redis中释放主机名锁
```
curl -N -s http://127.0.0.1:8080/v1/host/unregister \
-H 'Content-Type: application/json' \
-d '{"ID":"biz-goods","Hostname":"prod-goods-ms-001","IP":"10.1.2.3"}'
```