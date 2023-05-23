package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/KKKKjl/originalImage/opt"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	defaultTimeout    = 20 * time.Second
	defaultWorkersNum = 1024
)

var (
	client = &http.Client{
		Timeout: defaultTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
)

func main() {
	if err := opt.MustInitConfig(); err != nil {
		log.Fatal(err)
	}

	s3, err := NewS3Client(
		opt.Cfg.Endpoint,
		opt.Cfg.AccessKey,
		opt.Cfg.SecretAccessKey,
		opt.Cfg.BucketName,
		defaultTimeout,
	)
	if err != nil {
		log.Fatalf("create s3 client error: %v", err)
	}

	var (
		mux = http.NewServeMux()
		srv = http.Server{
			Addr:    opt.Cfg.Addr,
			Handler: mux,
		}
		closed  = make(chan struct{})
		sig     = make(chan os.Signal, 1)
		errCh   = make(chan error, 1)
		workers = make(chan string, defaultWorkersNum)
	)

	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	mux.HandleFunc("/api/images", func(w http.ResponseWriter, r *http.Request) {
		var (
			resp BaseResponse
			args RequestArgs
		)

		if strings.ToUpper(r.Method) != "POST" {
			resp.RespErr(w, http.StatusMethodNotAllowed, "Only method POST is allowed")
			return
		}

		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			resp.RespErr(w, http.StatusBadRequest, err.Error())
			return
		}

		uri, err := url.Parse(args.Url)
		if err != nil {
			resp.RespErr(w, http.StatusBadRequest, "Invalid parameters")
			return
		}

		// https://weibo.com/7470197961/N1DZ3ecrO?refer_flag=1001030103_
		parts := strings.Split(uri.Path, "/")
		if len(parts) == 0 {
			resp.RespErr(w, http.StatusBadRequest, "Invalid parameters")
			return
		}

		if args.Cookie != "" {
			_ = opt.Cfg.UpdateCookie(args.Cookie)
		}

		target := "https://weibo.com/ajax/statuses/show?id=" + parts[len(parts)-1]
		res, err := fetchOriginalUrls(
			client,
			target,
			opt.Cfg.GetCookie(),
		)
		if err != nil {
			log.Printf("fetch original urls [%s] err: %v", target, err)
			resp.RespErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		for _, id := range res.Pic_ids {
			url := "https://lz.sinaimg.cn/oslarge/" + id + ".jpg"
			select {
			case workers <- url:
			default:
				log.Printf("enqueue timeout, url %s", url)
			}
		}

		resp.RespOK(w, res)
	})

	go func() {
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

		log.Printf("listen on %s", opt.Cfg.Addr)
		if err := srv.ListenAndServe(); err != nil {
			errCh <- err
			return
		}
		log.Printf("Stop handling new requests")
	}()

	go consume(closed, workers, s3)

	select {
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	case <-sig:
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("shutdown server error: %v", err)
		}
		log.Printf("server shutdown")
	}

	close(closed)
}

func consume(stop <-chan struct{}, workers <-chan string, s3 *S3Client) {
	for {
		select {
		case <-stop:
			return
		case url := <-workers:
			Go(func() {
				reader, size, err := fetchOriginalImage(client, url, opt.Cfg.GetCookie())
				if err != nil {
					log.Printf("fetch original image %s error: %v", url, err)
					return
				}
				defer reader.Close()

				var (
					objectName string
					parts      = strings.Split(url, "/")
				)

				if len(parts) > 0 {
					objectName = parts[len(parts)-1]
				} else {
					objectName, _ = generateRandomFileName()
					objectName += ".jpg"
				}

				if _, err = s3.PutObject(objectName, reader, size, &minio.PutObjectOptions{
					ContentType: "image/jpeg",
				}); err != nil {
					log.Printf("put object %s error: %v", objectName, err)
					return
				}
			})
		}
	}
}

func fetchOriginalUrls(client *http.Client, url string, cookie string) (*WeiboResponse, error) {
	resp, err := MakeRequest(client, url, map[string]string{
		"accept":          "application/json, text/plain, */*",
		"accept-language": "zh-CN,zh;q=0.9",
		"cookie":          cookie,
		"user-agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.0.0 Safari/537.36",
	})
	if err != nil {
		return nil, fmt.Errorf("make request to %s err: %w", url, err)
	}
	defer resp.Body.Close()

	var weiboResp WeiboResponse
	if err := json.NewDecoder(resp.Body).Decode(&weiboResp); err != nil {
		return nil, fmt.Errorf("decode weibo resp err: %w", err)
	}

	return &weiboResp, nil
}

func fetchOriginalImage(client *http.Client, url string, cookie string) (io.ReadCloser, int64, error) {
	resp, err := MakeRequest(client, url, map[string]string{
		"accept":          "application/json, text/plain, */*",
		"accept-language": "zh-CN,zh;q=0.9",
		"cookie":          cookie,
		"referer":         url,
		"user-agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.0.0 Safari/537.36",
	})
	if err != nil {
		return nil, -1, fmt.Errorf("make request to %s err: %w", url, err)
	}

	return resp.Body, resp.ContentLength, nil
}

func MakeRequest(client *http.Client, url string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	return client.Do(req)
}

type (
	RequestArgs struct {
		Url    string `json:"url"`
		Cookie string `json:"cookie"`
	}

	WeiboResponse struct {
		Pic_num   int                    `json:"pic_num"`
		Pic_ids   []string               `json:"pic_ids"`
		Pic_infos map[string]interface{} `json:"pic_infos"`
	}

	BaseResponse struct {
		Code int         `json:"code"`
		Msg  string      `json:"msg"`
		Data interface{} `json:"data"`
	}
)

func (w *BaseResponse) RespOK(rw http.ResponseWriter, data interface{}) {
	w.Code = 0
	w.Msg = "success"
	w.Data = data

	buf, err := json.Marshal(w)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte("json marshal error"))
		return
	}

	rw.WriteHeader(http.StatusOK)
	rw.Write(buf)
}

func (w *BaseResponse) RespErr(rw http.ResponseWriter, code int, msg string) {
	w.Code = code
	w.Msg = msg

	buf, err := json.Marshal(w)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte("json marshal error"))
		return
	}

	rw.WriteHeader(code)
	rw.Write(buf)
}

type S3Client struct {
	timeout time.Duration
	bucket  string
	*minio.Client
}

func NewS3Client(endpoint, accessKeyID, secretAccessKey, bucket string, timeout time.Duration) (*S3Client, error) {
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: opt.Cfg.Secure,
	})
	if err != nil {
		return nil, err
	}

	return &S3Client{
		timeout: timeout,
		bucket:  bucket,
		Client:  minioClient,
	}, nil
}

func (s *S3Client) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	return s.Client.BucketExists(ctx, bucketName)
}

func (s *S3Client) PutObject(objectName string, reader io.Reader, objectSize int64, opts *minio.PutObjectOptions) (minio.UploadInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	isExists, err := s.BucketExists(ctx, s.bucket)
	if err != nil {
		return minio.UploadInfo{}, err
	}

	if !isExists {
		return minio.UploadInfo{}, fmt.Errorf("bucket %s not exists", s.bucket)
	}

	return s.Client.PutObject(ctx, s.bucket, objectName, reader, objectSize, *opts)
}

func Go(fn func()) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic: %v", err)
			}
		}()

		fn()
	}()
}

func generateRandomFileName() (string, error) {
	randomBytes := make([]byte, 16)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", err
	}

	fileName := hex.EncodeToString(randomBytes)
	return fileName, nil
}
