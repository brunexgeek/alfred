# Alfred

**Alfred** is a lightweight tool for managing and organizing product documentation as static websites.

It automatically structures documentation into a consistent directory hierarchy and generates index pages to make navigation simple and intuitive. The output can be served using any web server such as Nginx or Apache HTTP Server.

Alfred helps you manage documentation by:

* Organizing documentation into a structured hierarchy
* Automatically generating index pages for easy navigation, with template support
* Supporting multiple products, languages e formats in the same structure
* Isolating documentation per environment (e.g., internal, production)
* Offering REST endpoint to publish content

Alfred organizes content using the following hierarchy for each environment:

```
/<product>
  /<language>
    /<version>
      /<format>
        Your content here
```

Each environment can be stored in a different place, only the structure inside it is managed by Alfred.

## Configuration

A minimal configuration looks like the following:

```json
{
    "manager": {
        "host": "127.0.0.1",
        "port": 7000
    },
    "environments": [
        {
            "name": "production",
            "path": "/tmp/prod",
        }
    ]
}
```

The `manager` entry defines the host and port in which Alfred will offer its REST endpoints. This is intented for internal use, **do not** expose this to the internet. The `environments` entry allows you to define how many environments you want. Usually you'll serve each environment in a distinct host/port.

Environments have the following fields:

* **name:** Unique name of the environment, used when publishing content via REST endpoint.
* **path:** Path to the directory where the environment content should be stored. Prefer absolute paths and make sure the user used to run Alfred have write permissions on it.
* **templates:** Optional map of templates to be used for each index page. The valid entries are: `products`, `languages`, `versions` and `formats`. Templates are written using [Go template syntax](https://pkg.go.dev/html/template). Check the directory `templates` in the source repository for some examples.
* **strings:** Optional map of substitutions. Alfred will try to populate the template context with values using this map. You can also get substitutions manually by calling `.Strings.Get` in the template context. This map is useful to replace a product ID (e.g., `my-product`) to its marketing name (.e.g, `My Super Product Plus`), specially because product IDs have a limited set of allowed characters.

## Building

Clone this repository and run the `build.sh` script. Make sure you have Go compiler installed. The executable will be placed at `/tmp/alfred`.

```bash
git clone https://github.com/brunexgeek/alfred.git
cd alfred
bash ./build.sh
```

You can also create the docker image locally using the script `docker/create.sh`:

```bash
bash ./docker/create.sh
```

## Usage

To run Alfred in your host machine, use:

```bash
./alfred config.json
```

To run using Docker, use something like the following. Keep in mind that you need to match the Docker mappings to the paths you specified in the `config.json`. The following example assumes there's the environment `env1` at `/docs/env1`.

```bash
docker run --rm -v ~/docs/env1:/docs/env -v ~/config.json:/opt/config.json brunexgeek/alfred:0.1.0
```

## Publishing content

It's possible to publish content through the built-in web page `http://<host>:<port>/`, where host and port are the values specified in the `manager` entry in the configuration file. This address also serves the REST endpoint `http://<host>:<port>/v1/publish`. The endpoint uploads metadata and a file attachment to Alfred, and expects a `multipart/form-data` request with two fields:

* **params**: JSON object containing metadata of the content being published:

  * **env**: Target environment, as defined in the configuration file.
  * **prod**: Product identifier. A new directory hierarchy will be created if the product do not exists yet.
  * **ver**: Semantic version of the product and optional tag. The tag is placed after the version, preceded by a dash (e.g., `1.2.11-mytag`).
  * **fmt**: Content format. Valid values are `html`, `tgz` and `pdf`. If it's `html`, the content of the fuploaded file (must be a `tag.gz`) will be extracted into the directory hierarchy.
  * **lang**: Language code. Valid values are `en`, `es` and `ptr`.

* **attachment**: File being uploaded. Supported types are `pdf` and `tar.gz`.

Example:

```bash
curl -v -X POST -F "params={\"env\":\"production\",\"prod\":\"my-product\",\"ver\":\"1.2.11\",\"fmt\":\"html\",\"lang\":\"en\",\"email\":\"admin@example.com\"}" -F attachment=@package.tar.gz "http://127.0.0.1:7000/v1/publish"
```

## License

Except where explicitly indicated otherwise, all source codes of this project are provide under [Apache License 2.0](http://www.apache.org/licenses/LICENSE-2.0).
