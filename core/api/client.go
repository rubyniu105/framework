/* Copyright © INFINI LTD. All rights reserved.
 * Web: https://infinilabs.com
 * Email: hello#infini.ltd */

package api

import (
	"crypto/tls"
	"crypto/x509"
	"infini.sh/framework/core/config"
	"io/ioutil"
)

func connect() {

	//// TLS证书解析验证
	//if _, err = tls.LoadX509KeyPair(G_config.ServerPem, G_config.ServerKey); err != nil {
	//	//return common.ERR_CERT_INVALID
	//}
	//
	//transport := &http.Transport{
	//	TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, // 不校验服务端证书
	//	MaxIdleConns:        G_config.GatewayMaxConnection,
	//	MaxIdleConnsPerHost: G_config.GatewayMaxConnection,
	//	IdleConnTimeout:     time.Duration(G_config.GatewayIdleTimeout) * time.Second, // 连接空闲超时
	//}
	//
	//// 启动HTTP/2协议
	//http2.ConfigureTransport(transport)
	//
	//// HTTP/2 客户端
	//gateConn.client = &http.Client{
	//	Transport: transport,
	//	Timeout:   time.Duration(G_config.GatewayTimeout) * time.Millisecond, // 请求超时
	//}
}


func GetFastHTTPClientTLSConfig(tlsConfig *config.TLSConfig)*tls.Config  {
	if tlsConfig != nil {

		var cfg *tls.Config
		if tlsConfig.TLSInsecureSkipVerify {
			cfg = &tls.Config{
				InsecureSkipVerify: tlsConfig.TLSInsecureSkipVerify,
			}
		} else {
			caCert, err := ioutil.ReadFile(tlsConfig.TLSCACertFile)
			if err != nil {
				panic(err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				panic("failed to load ca cert")
			}

			cert, err := tls.LoadX509KeyPair(tlsConfig.TLSCertFile, tlsConfig.TLSKeyFile)
			if err != nil {
				panic(err)
			}

			cfg = &tls.Config{
				ServerName: tlsConfig.DefaultDomain,

				//for client
				RootCAs:            caCertPool,
				ClientSessionCache: tls.NewLRUClientSessionCache(tlsConfig.ClientSessionCacheSize),
				Certificates:       []tls.Certificate{cert},
			}

			if cfg.ServerName == "" {
				cfg.ServerName = "localhost"
			}

			cfg.Certificates = append(cfg.Certificates, cert)
			cfg.BuildNameToCertificate()
		}
	}
	return nil
}