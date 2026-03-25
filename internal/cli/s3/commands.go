package s3

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newMBCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:     "mb",
		Short:   "Make a new S3 bucket",
		Example: `  tarn s3 mb --name my-bucket`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			req, err := http.NewRequest(http.MethodPut, s3URL(endpoint, name), nil)
			if err != nil {
				return err
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			fmt.Printf("Bucket created: s3://%s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Bucket name (required)")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newRBCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:     "rb",
		Short:   "Remove an S3 bucket",
		Example: `  tarn s3 rb --name my-bucket`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			req, err := http.NewRequest(http.MethodDelete, s3URL(endpoint, name), nil)
			if err != nil {
				return err
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			fmt.Printf("Bucket removed: s3://%s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Bucket name (required)")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newLsCmd() *cobra.Command {
	var bucket string

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List buckets or objects in a bucket",
		Example: `  tarn s3 ls
  tarn s3 ls --bucket my-bucket`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			if bucket == "" {
				return listBuckets(endpoint)
			}
			return listObjects(endpoint, bucket)
		},
	}

	cmd.Flags().StringVar(&bucket, "bucket", "", "Bucket name (omit to list all buckets)")
	return cmd
}

func listBuckets(endpoint string) error {
	resp, err := http.Get(s3URL(endpoint, ""))
	if err != nil {
		return fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		XMLName xml.Name `xml:"ListAllMyBucketsResult"`
		Buckets []struct {
			Name         string `xml:"Name"`
			CreationDate string `xml:"CreationDate"`
		} `xml:"Buckets>Bucket"`
	}
	if err := xml.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Buckets) == 0 {
		fmt.Println("No buckets found.")
		return nil
	}

	for _, b := range result.Buckets {
		fmt.Printf("%s  s3://%s\n", b.CreationDate, b.Name)
	}
	return nil
}

func listObjects(endpoint, bucket string) error {
	resp, err := http.Get(s3URL(endpoint, bucket+"?list-type=2"))
	if err != nil {
		return fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		XMLName  xml.Name `xml:"ListBucketResult"`
		Contents []struct {
			Key          string `xml:"Key"`
			Size         int64  `xml:"Size"`
			LastModified string `xml:"LastModified"`
		} `xml:"Contents"`
	}
	if err := xml.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Contents) == 0 {
		fmt.Println("No objects found.")
		return nil
	}

	for _, obj := range result.Contents {
		fmt.Printf("%s  %10d  %s\n", obj.LastModified, obj.Size, obj.Key)
	}
	return nil
}

func newCpCmd() *cobra.Command {
	var (
		bucket string
		key    string
		file   string
	)

	cmd := &cobra.Command{
		Use:     "cp",
		Short:   "Upload a file to S3",
		Example: `  tarn s3 cp --bucket my-bucket --key hello.txt --file ./hello.txt`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			f, err := os.Open(file)
			if err != nil {
				return fmt.Errorf("open file: %w", err)
			}
			defer f.Close()

			url := s3URL(endpoint, bucket+"/"+key)
			req, err := http.NewRequest(http.MethodPut, url, f)
			if err != nil {
				return err
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			fmt.Printf("Uploaded: s3://%s/%s\n", bucket, key)
			return nil
		},
	}

	cmd.Flags().StringVar(&bucket, "bucket", "", "Bucket name (required)")
	cmd.Flags().StringVar(&key, "key", "", "Object key (required)")
	cmd.Flags().StringVar(&file, "file", "", "Local file path (required)")
	cmd.MarkFlagRequired("bucket")
	cmd.MarkFlagRequired("key")
	cmd.MarkFlagRequired("file")
	return cmd
}

func newGetCmd() *cobra.Command {
	var (
		bucket string
		key    string
		output string
	)

	cmd := &cobra.Command{
		Use:     "get",
		Short:   "Download an object from S3",
		Example: `  tarn s3 get --bucket my-bucket --key hello.txt --output ./hello.txt`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			url := s3URL(endpoint, bucket+"/"+key)
			resp, err := http.Get(url)
			if err != nil {
				return fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			if output == "" {
				output = filepath.Base(key)
			}

			f, err := os.Create(output)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}
			defer f.Close()

			n, err := io.Copy(f, resp.Body)
			if err != nil {
				return fmt.Errorf("write file: %w", err)
			}

			fmt.Printf("Downloaded: %s (%d bytes)\n", output, n)
			return nil
		},
	}

	cmd.Flags().StringVar(&bucket, "bucket", "", "Bucket name (required)")
	cmd.Flags().StringVar(&key, "key", "", "Object key (required)")
	cmd.Flags().StringVar(&output, "output", "", "Output file path (default: basename of key)")
	cmd.MarkFlagRequired("bucket")
	cmd.MarkFlagRequired("key")
	return cmd
}

func newRmCmd() *cobra.Command {
	var (
		bucket string
		key    string
	)

	cmd := &cobra.Command{
		Use:     "rm",
		Short:   "Delete an object from S3",
		Example: `  tarn s3 rm --bucket my-bucket --key hello.txt`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			url := s3URL(endpoint, bucket+"/"+key)
			req, err := http.NewRequest(http.MethodDelete, url, nil)
			if err != nil {
				return err
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			fmt.Printf("Deleted: s3://%s/%s\n", bucket, key)
			return nil
		},
	}

	cmd.Flags().StringVar(&bucket, "bucket", "", "Bucket name (required)")
	cmd.Flags().StringVar(&key, "key", "", "Object key (required)")
	cmd.MarkFlagRequired("bucket")
	cmd.MarkFlagRequired("key")
	return cmd
}
