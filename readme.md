Godrive
=======

**Godrive** is a lightweight Golang utility designed to solve a common headache: backing up your entire Google Drive.

If you have ever tried to download your whole Drive to an external hard drive, you know the struggle. Google Takeout often splits your data into dozens of confused `.zip` files, making organization a nightmare. Godrive connects directly to the Google Drive API and downloads your files in their original format and structure, directly to your target directory.

🚀 Features
-----------

-   **Direct Download**: Bypasses the need for zipping files.

-   **Parallel Processing**: Uses Go routines to download multiple files simultaneously for faster throughput.

-   **Folder Structure**: Preserves your Google Drive folder hierarchy locally.

🛠️ Prerequisites
-----------------

-   [Go](https://go.dev/dl/ "null") installed on your machine.

-   A Google Cloud Project with the **Google Drive API** enabled.

⚙️ Configuration & Setup
------------------------

### 1\. Clone the Repository

```
git clone [https://github.com/Hoffmano/godrive.git](https://github.com/Hoffmano/godrive.git)
cd godrive

```

### 2\. Get Google Credentials

You need to authorize the application to access your Google Drive.

1.  Go to the [Google Drive API Quickstart for Go](https://developers.google.com/workspace/drive/api/quickstart/go?hl=pt-br "null").

2.  Follow the steps to "Configure the OAuth consent screen" and "Create credentials".

3.  Download the `credentials.json` file.

4.  Save the `credentials.json` file in the root directory of this project.

### 3\. Configure the Script

Open `main.go` in your text editor and modify the constants block to match your needs:

```
const (
    // Quantity of files being downloaded in parallel.
    // Caution: Setting this too high may hit API rate limits or saturate your network.
    numWorkers      = 1000

    // Your target path (e.g., your external HD mount point).
    // Ensure this path exists and you have write permissions.
    downloadPath    = "/media/ghs/hd/godrive2/"

    // ID of the specific folder you want to download.
    // Leave empty "" to download the entire Google Drive root folder.
    driveFolderPath = ""
)

```

🏃 Usage
--------

Once configured, run the application using Go:

```
go mod tidy  # Download dependencies
go run main.go

```

The script will begin authenticating (you may need to click a link in your terminal to log in via browser for the first run) and then start downloading your files to the specified `downloadPath`.

⚠️ Important Notes
------------------

-   **Rate Limiting**: The default `numWorkers` is set to 1000. If you experience errors regarding API rate limits (403 errors), try reducing this number.

-   **Storage**: Ensure your target drive has enough free space to accommodate your Google Drive contents.

🤝 Contributing
---------------

Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

📄 License
----------

[MIT](https://choosealicense.com/licenses/mit/ "null")
