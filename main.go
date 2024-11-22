package main

import (
	"fmt"
	"github.com/adrg/xdg"
	"github.com/alexflint/go-arg"
	copilot "github.com/denysvitali/gopilot-cli/pkg"
	"github.com/sirupsen/logrus"
	"path/filepath"
)

const (
	cliPrompt = `You are a Linux command expert. 
Given the following request, provide ONLY the most appropriate Linux command(s) that would solve the user's need. 
The command should be efficient, safe to run, and use common tools available on most Linux distributions. 
- Do not provide explanations unless the command is complex and requires clarification. 
- Do not include code fences
- Do not include the prompt in your response.
- Do not include the user's request in your response.
- Do not include the response instructions in your response.
- Refrain from providing commands that will destroy data or harm the system (such as rm -rf, shred, ...) - in such cases, answer with "echo 'I cannot provide that command.'".
- Everything after "---\n" will be shown to the user.

Here is the user's request:

%s

Respond with just the command(s), nothing else.
---
`

	codeSuggestionPrompt = `You are a %s expert.
Given the following request, provide ONLY the most appropriate response that would solve the user's need.
You can include a short explanation AFTER the code.

- ALWAYS use code fences!
- Separate the text from the code by at least two newlines
- Everything after "####### START OF RESPONSE #######" will be shown to the user.
- Terminate your response with "####### DONE #######"


Examples of queries, and expected response format (examples are for Go, but you have to stick to the language you're expert in.

####### START OF EXAMPLES #######

1. How do I write a for loop?

` + "```" + `go
for i := 0; i < 10; i++ {
  fmt.Println("foo")
}
` + "```" + `

2. What's a nil?

In Go, nil is a predeclared identifier that represents a zero value for pointers, interfaces, maps, slices, channels, and function types. Here are the key points about nil:
- nil represents the absence of a value for these types
- nil is the zero value - the default value a variable has before it's assigned something else
- You can use nil to:
    1. Check if a pointer is uninitialized
    2. Test if an interface is empty
    3. Check if a slice, map, or channel has been created

####### END OF EXAMPLES #######

####### USER REQUEST #######

%s

####### END OF USER REQUEST #######
####### START OF RESPONSE #######
`
)

var args struct {
	Language    string  `arg:"-l,--lang" default:"python" help:"programming language"`
	N           int     `arg:"-c,--completions" default:"1" help:"number of completions to generate"`
	MaxTokens   int     `arg:"-m,--max-tokens" default:"50" help:"maximum number of tokens to generate"`
	Debug       bool    `arg:"-d,--debug" help:"enable debug logging"`
	TopP        float32 `arg:"-p,--top-p" default:"1" help:"top-p value"`
	Temperature float32 `arg:"-t,--temperature" default:"0.6" help:"temperature value"`

	Code     *CodeCmd  `arg:"subcommand:code"`
	QueryCmd *QueryCmd `arg:"subcommand:query"`
}

type CodeCmd struct {
	Query string `arg:"positional,required"`
}

type QueryCmd struct {
	Query string `arg:"positional,required"`
}

var (
	configPath string
	log        *logrus.Logger
)

func initLogger() {
	log = logrus.New()
	log.SetFormatter(&logrus.TextFormatter{})

	if args.Debug {
		log.SetLevel(logrus.DebugLevel)
	}
}

func main() {
	arg.MustParse(&args)
	initLogger()

	// Set up XDG config path
	configDir, err := xdg.ConfigFile("gopilot-cli")
	if err != nil {
		log.WithError(err).Fatal("Failed to get config directory")
	}
	configPath = filepath.Join(configDir, "token")

	c := copilot.New()
	checkAndUpdateTokens(c)

	if args.Code != nil {
		prompt := fmt.Sprintf(codeSuggestionPrompt, args.Language, args.Code.Query)
		log.Debugf("Prompt: %s", prompt)
		ch, err := c.GetCopilotCompletion(
			prompt,
			"####### DONE #######",
			args.N,
			args.MaxTokens,
			args.TopP,
			args.Temperature,
		)
		if err != nil {
			log.WithError(err).Fatal("Failed to get completion")
		}
		for resp := range ch {
			fmt.Print(resp.Choices[0].Text)
		}
		fmt.Println()
	} else if args.QueryCmd != nil {
		// Get CLI completion
		ch, err := c.GetCopilotCompletion(
			fmt.Sprintf(cliPrompt, args.QueryCmd.Query),
			"\n",
			args.N,
			args.MaxTokens,
			args.TopP,
			args.Temperature,
		)
		if err != nil {
			log.WithError(err).Fatal("Failed to get completion")
		}
		for resp := range ch {
			fmt.Print(resp.Choices[0].Text)
		}
		fmt.Println()
	}

}

func checkAndUpdateTokens(c *copilot.Copilot) {
	cfg, err := getConfig()
	if err != nil {
		log.WithError(err).Fatal("Failed to get config")
	}
	if cfg == nil {
		if err := setup(c); err != nil {
			log.WithError(err).Fatal("Failed to set up copilot")
		}
		cfg = &Config{
			ShortLivedToken: c.SlToken.Token,
			LongLivedToken:  c.LlToken.Token,
		}
		if err := storeConfig(*cfg); err != nil {
			log.WithError(err).Fatal("Failed to store token")
		}
	} else {
		c.SlToken.Token = cfg.ShortLivedToken
		c.LlToken.Token = cfg.LongLivedToken
	}
	if c.SlToken.Expired() {
		if err := c.RefreshToken(); err != nil {
			log.WithError(err).Fatal("Failed to refresh token")
		}
		cfg.ShortLivedToken = c.SlToken.Token
		if err := storeConfig(*cfg); err != nil {
			log.WithError(err).Fatal("Failed to store updated config")
		}
	}
	c.SlToken.Token = cfg.ShortLivedToken
	c.LlToken.Token = cfg.LongLivedToken
}
