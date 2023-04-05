# jsonpart

Get partial json value in mix string, as json embedded in html. Code from fastjson


```go
    s := `<!DOCTYPE html>
          <html>
            <head>
          	  <title>test</title>
            </head>
            <body>
              <script>
                var Data = {
                  "test": {
                    "head": "show",
                    "value": 18,
                    "ctx": {
                      "service": "feekback",
                      "params": ["a", "b", "c"],
                      "log": true,
                      "num": 10
                    }
                  }
                };
              </script>
            </body>
          </html>`
    v, err := jsonpart.Parse(s, "ctx")
    if err != nil {
    	fmt.Print(err)
    	return
    }
    fmt.Println(v.String())
    fmt.Println(v.GetString("service"))
    fmt.Println(v.GetString("params", "1"))
    fmt.Println(v.GetBool("log"))
    fmt.Println(v.GetInt("num"))
```