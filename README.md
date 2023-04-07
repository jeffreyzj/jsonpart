# jsonpart

Get partial json value in mix string, as json embedded in html. Code from fastjson.


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
	fmt.Println(v.MarshalString()) //output: {"service":"feekback","params":["a","b","c"],"log":true,"num":10}
	fmt.Println(v.GetString("service")) //output: feekback
	fmt.Println(v.GetString("params", "1")) //output: b
	fmt.Println(v.GetBool("log")) //output: true
	fmt.Println(v.GetInt("num")) //output: 10
```

Get full json value

```go
    s := `{
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
          }`
    v, err := jsonpart.Parse(s)
    if err != nil {
    	fmt.Print(err)
    	return
    }
	fmt.Println(v.GetString("test", "head")) //output: show
	fmt.Println(v.GetString("test", "ctx", "params", "1")) //output: b
	fmt.Println(v.GetBool("test", "ctx", "log")) //output: true
	fmt.Println(v.GetInt("test", "value")) //output: 18
```