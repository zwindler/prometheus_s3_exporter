package cmd

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
)

const (
	namespace = "s3"
)

var (
	listenAddr         string
	metricsPath        string
	buckets            string
	credentialsMapping string
	prefix             string
	delimiter          string
	endpointURL        string
	region             string
	sse                string
	disableSSL         bool
	forcePathStyle     bool

	s3ListSuccess = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "list_success"),
		"If the ListObjects operation was a success",
		[]string{"bucket", "prefix", "delimiter"}, nil,
	)
	s3ListDuration = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "list_duration_seconds"),
		"The total duration of the list operation",
		[]string{"bucket", "prefix", "delimiter"}, nil,
	)
	s3LastModifiedObjectDate = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "last_modified_object_date"),
		"The last modified date of the object that was modified most recently",
		[]string{"bucket", "prefix"}, nil,
	)
	s3LastModifiedObjectSize = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "last_modified_object_size_bytes"),
		"The size of the object that was modified most recently",
		[]string{"bucket", "prefix"}, nil,
	)
	s3ObjectTotal = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "objects"),
		"The total number of objects for the bucket/prefix combination",
		[]string{"bucket", "prefix"}, nil,
	)
	s3SumSize = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "objects_size_sum_bytes"),
		"The total size of all objects summed",
		[]string{"bucket", "prefix"}, nil,
	)
	s3BiggestSize = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "biggest_object_size_bytes"),
		"The size of the biggest object",
		[]string{"bucket", "prefix"}, nil,
	)
	s3CommonPrefixes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "common_prefixes"),
		"A count of all the keys between the prefix and the next occurrence of the string specified by the delimiter",
		[]string{"bucket", "prefix", "delimiter"}, nil,
	)
)

type Exporter struct {
	buckets        []string
	prefix         string
	delimiter      string
	credentialsMap map[string]Credentials
	endpointURL    string
	region         string
	disableSSL     bool
	forcePathStyle bool
}

type Credentials struct {
	AccessKey     string
	SecretKeyFile string
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- s3ListSuccess
	ch <- s3ListDuration
	if e.delimiter == "" {
		ch <- s3LastModifiedObjectDate
		ch <- s3LastModifiedObjectSize
		ch <- s3ObjectTotal
		ch <- s3SumSize
		ch <- s3BiggestSize
	} else {
		ch <- s3CommonPrefixes
	}
}

// Collect metrics
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	var wg sync.WaitGroup

	for _, bucket := range e.buckets {
		wg.Add(1)
		go func(bucket string) {
			defer wg.Done()
			e.collectMetricsForBucket(bucket, ch)
		}(bucket)
	}

	wg.Wait()
}

func (e *Exporter) collectMetricsForBucket(bucket string, ch chan<- prometheus.Metric) {
	var lastModified time.Time
	var numberOfObjects float64
	var totalSize int64
	var biggestObjectSize int64
	var lastObjectSize int64
	var commonPrefixes int

	var creds *credentials.Credentials
	if e.credentialsMap != nil {
		credsInfo, ok := e.credentialsMap[bucket]
		if !ok {
			log.Fatalf("no credentials found for bucket: %s", bucket)
		}

		secretKey, err := os.ReadFile(credsInfo.SecretKeyFile)
		if err != nil {
			log.Fatalf("error reading secret key file for bucket %s: %v", bucket, err)
		}

		creds = credentials.NewStaticCredentials(credsInfo.AccessKey, strings.TrimSpace(string(secretKey)), "")
	} else {
		creds = credentials.NewChainCredentials(
			[]credentials.Provider{
				&credentials.EnvProvider{},
				&credentials.SharedCredentialsProvider{},
			})
	}

	sess, err := session.NewSession(&aws.Config{
		Credentials:      creds,
		Endpoint:         aws.String(e.endpointURL),
		Region:           aws.String(e.region),
		DisableSSL:       aws.Bool(e.disableSSL),
		S3ForcePathStyle: aws.Bool(e.forcePathStyle),
	})
	if err != nil {
		log.Fatalf("error creating session: %v", err)
	}

	svc := s3.New(sess)

	query := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(e.prefix),
		Delimiter: aws.String(e.delimiter),
	}

	// Continue making requests until we've listed and compared the date of every object
	startList := time.Now()
	for {
		resp, err := svc.ListObjectsV2(query)
		if err != nil {
			log.Fatalf("error when listing objects: %v", err)
			ch <- prometheus.MustNewConstMetric(
				s3ListSuccess, prometheus.GaugeValue, 0, bucket, e.prefix,
			)
			return
		}
		commonPrefixes = commonPrefixes + len(resp.CommonPrefixes)
		for _, item := range resp.Contents {
			numberOfObjects++
			totalSize = totalSize + *item.Size
			if item.LastModified.After(lastModified) {
				lastModified = *item.LastModified
				lastObjectSize = *item.Size
			}
			if *item.Size > biggestObjectSize {
				biggestObjectSize = *item.Size
			}
		}
		if resp.NextContinuationToken == nil {
			break
		}
		query.ContinuationToken = resp.NextContinuationToken
	}
	listDuration := time.Since(startList).Seconds()

	ch <- prometheus.MustNewConstMetric(
		s3ListSuccess, prometheus.GaugeValue, 1, bucket, e.prefix, e.delimiter,
	)
	ch <- prometheus.MustNewConstMetric(
		s3ListDuration, prometheus.GaugeValue, listDuration, bucket, e.prefix, e.delimiter,
	)
	if e.delimiter == "" {
		ch <- prometheus.MustNewConstMetric(
			s3LastModifiedObjectDate, prometheus.GaugeValue, float64(lastModified.UnixNano()/1e9), bucket, e.prefix,
		)
		ch <- prometheus.MustNewConstMetric(
			s3LastModifiedObjectSize, prometheus.GaugeValue, float64(lastObjectSize), bucket, e.prefix,
		)
		ch <- prometheus.MustNewConstMetric(
			s3ObjectTotal, prometheus.GaugeValue, numberOfObjects, bucket, e.prefix,
		)
		ch <- prometheus.MustNewConstMetric(
			s3BiggestSize, prometheus.GaugeValue, float64(biggestObjectSize), bucket, e.prefix,
		)
		ch <- prometheus.MustNewConstMetric(
			s3SumSize, prometheus.GaugeValue, float64(totalSize), bucket, e.prefix,
		)
	} else {
		ch <- prometheus.MustNewConstMetric(
			s3CommonPrefixes, prometheus.GaugeValue, float64(commonPrefixes), bucket, e.prefix, e.delimiter,
		)
	}
}

var rootCmd = &cobra.Command{
	Use:   "prometheus_s3_exporter",
	Short: "Export metrics for S3 buckets",
	Long:  `Yet another Prometheus exporter for AWS S3 bucket metrics.`,
	Run: func(cmd *cobra.Command, args []string) {
		var bucketList []string
		var credentialsMap map[string]Credentials
		var err error

		if credentialsMapping != "" {
			credentialsMap, err = loadCredentialsMapping(credentialsMapping)
			if err != nil {
				log.Fatalf("error loading credentials mapping: %v", err)
			}
			for bucket := range credentialsMap {
				bucketList = append(bucketList, bucket)
			}
		} else if buckets != "" {
			bucketList = splitBuckets(buckets)
		} else {
			log.Fatalf("either --s3.buckets or --s3.credentials-mapping must be specified")
		}

		exporter := &Exporter{
			buckets:        bucketList,
			prefix:         prefix,
			delimiter:      delimiter,
			credentialsMap: credentialsMap,
			endpointURL:    endpointURL,
			region:         region,
			disableSSL:     disableSSL,
			forcePathStyle: forcePathStyle,
		}

		registry := prometheus.NewRegistry()
		registry.MustRegister(exporter)

		http.Handle(metricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`<html>
							 <head><title>AWS S3 Exporter</title></head>
							 <body>
							 <h1>AWS S3 Exporter</h1>
							 <p><a href='` + metricsPath + `'>Metrics</a></p>
							 </body>
							 </html>`))
		})

		log.Printf("Listening on %s", listenAddr)
		log.Fatal(http.ListenAndServe(listenAddr, nil))
	},
}

func loadCredentialsMapping(filePath string) (map[string]Credentials, error) {
	credentialsMap := make(map[string]Credentials)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid credentials mapping format")
		}
		bucket := parts[0]
		accessKey := parts[1]
		secretKeyFile := parts[2]
		credentialsMap[bucket] = Credentials{
			AccessKey:     accessKey,
			SecretKeyFile: secretKeyFile,
		}
	}

	return credentialsMap, nil
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&listenAddr, "web.listen-address", ":9340", "Address to listen on for web interface and telemetry.")
	rootCmd.PersistentFlags().StringVar(&metricsPath, "web.metrics-path", "/metrics", "Path under which to expose metrics")
	rootCmd.PersistentFlags().StringVar(&buckets, "s3.buckets", "", "Comma-separated list of S3 buckets to monitor")
	rootCmd.PersistentFlags().StringVar(&credentialsMapping, "s3.credentials-mapping", "", "Path to the credentials mapping file")
	rootCmd.PersistentFlags().StringVar(&prefix, "s3.prefix", "", "Prefix to filter objects")
	rootCmd.PersistentFlags().StringVar(&delimiter, "s3.delimiter", "", "Delimiter to group objects")
	rootCmd.PersistentFlags().StringVar(&endpointURL, "s3.endpoint-url", "", "Custom endpoint URL")
	rootCmd.PersistentFlags().StringVar(&region, "s3.region", "", "AWS region")
	rootCmd.PersistentFlags().StringVar(&sse, "s3.sse", "", "SSE version")
	rootCmd.PersistentFlags().BoolVar(&disableSSL, "s3.disable-ssl", false, "Custom disable SSL")
	rootCmd.PersistentFlags().BoolVar(&forcePathStyle, "s3.force-path-style", false, "Custom force path style")

	rootCmd.MarkPersistentFlagRequired("s3.region")
}

func initConfig() {
	// Any additional initialization can be done here
}

func splitBuckets(buckets string) []string {
	return strings.Split(buckets, ",")
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}
