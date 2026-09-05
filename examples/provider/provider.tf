terraform {
  required_providers {
    dataiku = {
      source  = "amrutp24/dataiku"
      version = "~> 0.1"
    }
  }
}

# The API key is read from the DATAIKU_API_KEY environment variable so that it
# never has to be written into configuration or state.
provider "dataiku" {
  host = "https://dss.example.com"
}
