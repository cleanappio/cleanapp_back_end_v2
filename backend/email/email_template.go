package email

import (
	"flag"
	"fmt"
)

const reportImgCid = "report-img"
const mapImgCid = "map-img"

var (
	optOutUrl          = flag.String("email_opt_out_url", "", "The email unsubscribing URL")
	cleanAppMapURL     = flag.String("cleanapp_map_url", "", "The URL of the CleanApp map")
	cleanAppAndroidURL = flag.String("cleanapp_android_url", "", "The URL of the CleanApp Android")
	cleanAppIOsURL     = flag.String("cleanapp_ios_url", "", "The URL of the CleanApp IOs")
)

func getOptOutURL(email string) string {
	return fmt.Sprintf("%s/optoutemail?email=%s", *optOutUrl, email)
}

func getEmailText(email string) string {
	return fmt.Sprintf(`
Hi,

Your email address is tagged to an area that just received this live CleanApp report. See the report image in the attachment.

Please take action as needed. If you received it in error, please unsubscribe here: %s

P.S.S.T. Get CleanApp on iOS (%s) or Android (%s) and visit CleanAppMap for real-time updates: %s.
`, getOptOutURL(email), *cleanAppIOsURL, *cleanAppAndroidURL, *cleanAppMapURL)
}

func getEmailHtml(email string) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>CleanAppMap Notes</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            margin: 40px;
            line-height: 1.6;
        }
        .container {
            max-width: 800px;
            margin: auto;
            padding: 20px;
            border: 1px solid #ddd;
            border-radius: 10px;
            background: #f9f9f9;
        }
        h1 {
            color: #2c3e50;
        }
        pre {
            background: #eee;
            padding: 10px;
            border-radius: 5px;
            overflow-x: auto;
        }
        a {
            color: #007bff;
            text-decoration: none;
        }
        a:hover {
            text-decoration: underline;
        }
        .report-image {
            padding: 20px;
            width: 200px;
            height: auto;
        }
        .map-image {
            padding: 20px;
            width: 200px;
            height: auto;
        }
    </style>
</head>
<body>
    <div class="container">
        <p>Hi,</p>
        <p>Your email address is tagged to an area that just received this live CleanApp report:</p>
		<img src="cid:%s" class="report-image" alt="CleanApp Report Image" />
		<img src="cid:%s" class="map-image" alt="CleanApp Report Image" />
        <p>Please take action as needed. If you received this in error, please <a href="%s">unsubscribe here</a>.</p>
        <p>💚 CleanApp</p>
        <p>P.S.S.T. Get CleanApp on <a href="%s">iOS</a> / <a href="%s">Android</a> and visit <a href="%s">CleanAppMap</a> for real-time updates.</p>
    </div>
</body>
</html>
`, reportImgCid, mapImgCid, getOptOutURL(email), *cleanAppIOsURL, *cleanAppAndroidURL, *cleanAppMapURL)
}
