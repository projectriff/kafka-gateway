/*
 * Copyright 2019 The original author or authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"encoding/json"
	"fmt"
	"github.com/Shopify/sarama"
	"log"
	"net/http"
	"os"
	"strings"
)

var (
	gateway = os.Getenv("GATEWAY")
	broker  = os.Getenv("BROKER")
)

func main() {

	if gateway == "" {
		log.Fatal("Environment variable GATEWAY should contain the host and port of a liiklus gRPC endpoint")
	}
	if broker == "" {
		log.Fatal("Environment variable BROKER should contain the host and port of a Kafka broker")
	}

	sarama.Logger = log.New(os.Stdout, "[Sarama] ", log.LstdFlags)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			handlePut(w, r)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	http.ListenAndServe(":8080", nil)
}

func handlePut(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path[1:], "/")
	if len(parts) != 2 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "URLs should be of the form /<namespace>/<stream-name>\n")
		return
	}

	config := sarama.NewConfig()
	config.Version = sarama.V0_11_0_0
	config.ClientID = "kafka-provisioner"
	admin, err := sarama.NewClusterAdmin([]string{broker}, config)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(os.Stderr, "Error connecting to Kafka broker %q: %v\n", broker, err)
		_, _ = fmt.Fprintf(w, "Error connecting to Kafka broker %q: %v\n", broker, err)
		return
	} else {
		defer func() {
			if err := admin.Close() ; err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error disconnecting from Kafka broker %q: %v\n", broker, err)
			}
		}()
	}

	// NOTE: choice of underscore as separator is important as it is not allowed in k8s names
	topicName := fmt.Sprintf("%s_%s", parts[0], parts[1])
	topicDetail := sarama.TopicDetail{NumPartitions: 1, ReplicationFactor: 1}
	if metadata, err := admin.DescribeTopics([]string{topicName}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(os.Stderr, "Error trying to list topics to see if %q exists: %v\n", topicName, err)
		_, _ = fmt.Fprintf(w, "Error trying to list topics to see if %q exists: %v\n", topicName, err)
		return
	} else if metadata[0].Err == sarama.ErrUnknownTopicOrPartition {
		if err := admin.CreateTopic(topicName, &topicDetail, false); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(os.Stderr, "Error creating topic %q: %v\n", topicName, err)
			_, _ = fmt.Fprintf(w, "Error creating topic %q: %v\n", topicName, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
	} else if metadata[0].Err == sarama.ErrNoError {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(os.Stderr, "Error creating topic %q: %v\n", topicName, err)
		_, _ = fmt.Fprintf(w, "Error creating topic %q: %v\n", topicName, err)
		return
	}

	// Either created or already existed
	w.Header().Set("Content-Type", "application/json")
	res := result{Gateway: gateway, Topic: topicName}
	if err := json.NewEncoder(w).Encode(res); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to write json response: %v", err)
		return
	} else {
		_, _ = fmt.Fprintf(os.Stderr, "Reported successful topic %q\n", topicName)
	}
}

type result struct {
	Gateway string `json:"gateway"`
	Topic   string `json:"topic"`
}
