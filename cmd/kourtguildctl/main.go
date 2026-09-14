// Command kourtguildctl is the operator's side of the Kourt ↔ Discord bridge.
//
// It prints the layout a court server gets, the permission set the bot asks for,
// and the two links a court moderator clicks — and it can apply that layout to a
// reference guild and mint the template every other server is created from.
//
//	kourtguildctl plan                       # the layout, as calls, printed
//	kourtguildctl perms                      # the permission set and its integer
//	kourtguildctl links --court meta ...     # the two links, for one court
//	kourtguildctl build-template --guild ID  # dry run: prints, changes nothing
//	kourtguildctl build-template --guild ID --apply
//
// DRY RUN IS THE DEFAULT, following kourtmod. build-template edits a live guild
// and mints a template that every court server afterwards is copied from, so the
// safe mode is the one you get by forgetting a flag. --dry-run is accepted too,
// so an operator can say the safe thing out loud in a script.
//
// THE BOT TOKEN IS NEVER A FLAG. It is read from KOURT_DISCORD_TOKEN, because a
// flag would put a credential that can ban people in every process listing on the
// box — the same reasoning that put the faucet's mnemonic in a systemd credential
// rather than an ExecStart argument.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jaekwon/kourt/internal/discordapi"
	"github.com/jaekwon/kourt/internal/guild"
)

const tokenEnv = "KOURT_DISCORD_TOKEN"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	verb := os.Args[1]
	args := os.Args[2:]

	var err error
	switch verb {
	case "plan":
		err = cmdPlan()
	case "perms":
		err = cmdPerms()
	case "links":
		err = cmdLinks(args)
	case "build-template":
		err = cmdBuildTemplate(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "kourtguildctl: unknown verb %q\n\n", verb)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "kourtguildctl: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `kourtguildctl — the operator's side of the Kourt ↔ Discord bridge

  plan                    print the court layout as the calls that would build it
  perms                   print the permission set the bot asks for, and its integer
  links                   print the two links a court moderator clicks
  build-template          apply the layout to a reference guild and mint its template

`)
}

// cmdPlan and cmdPerms take no flags and touch no network. They exist so the two
// things hardest to review by reading a struct — the order calls go out in, and an
// eighteen-bit integer — can be looked at.
func cmdPlan() error {
	ops, errs := guild.Plan(guild.CourtLayout())
	for _, op := range ops {
		fmt.Println(op)
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  invalid: %v\n", e)
		}
		return fmt.Errorf("the layout has %d problem(s)", len(errs))
	}
	fmt.Printf("\n%d call(s); the layout is consistent\n", len(ops))
	return nil
}

func cmdPerms() error {
	fmt.Print(guild.Explain())
	fmt.Printf("\n%-26s %s\n", "TOTAL", guild.RequiredBits())
	fmt.Printf("%-26s %d bits\n", "", len(guild.Required))
	return nil
}

func cmdLinks(argv []string) error {
	fs := flag.NewFlagSet("links", flag.ContinueOnError)
	court := fs.String("court", "", "court slug, as the chain spells it")
	chain := fs.String("chain", "kourt-1", "chain id the court lives on")
	clientID := fs.String("client-id", "", "the Discord application's client id")
	redirect := fs.String("redirect", "", "the registered OAuth redirect uri (https)")
	template := fs.String("template", "", "the server template code, if one has been minted")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *court == "" || *clientID == "" || *redirect == "" {
		return fmt.Errorf("links needs --court, --client-id and --redirect")
	}

	// THE NONCE IS MINTED HERE ONLY BECAUSE THIS IS THE CLI.
	//
	// In the real flow the service mints it, stores it against a short-lived
	// session and redeems it once — that is what makes it proof the redirect
	// belongs to a browser that started the flow. A link printed at a terminal
	// has no session behind it, so this one is single-use by convention and
	// nothing enforces it. Said out loud because a nonce nobody redeems is the
	// kind of thing that quietly becomes the production design.
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	st := guild.State{Chain: *chain, Court: *court, Nonce: hex.EncodeToString(b)}

	auth, err := guild.AuthorizeURL(*clientID, *redirect, st)
	if err != nil {
		return err
	}
	if *template != "" {
		t, err := guild.TemplateURL(*template)
		if err != nil {
			return err
		}
		fmt.Printf("1. create the server\n   %s\n\n", t)
	} else {
		fmt.Printf("1. create the server\n   (no template minted yet — run build-template)\n\n")
	}
	fmt.Printf("2. add the bot\n   %s\n\n", auth)
	fmt.Printf("Both portal settings must be on or step 2 silently does nothing:\n")
	fmt.Printf("  Public Bot, and Require OAuth2 Code Grant.\n")
	return nil
}

func cmdBuildTemplate(argv []string) error {
	fs := flag.NewFlagSet("build-template", flag.ContinueOnError)
	guildID := fs.String("guild", "", "the reference guild to lay out and snapshot")
	apply := fs.Bool("apply", false, "actually create the roles, channels and template")
	dry := fs.Bool("dry-run", false, "print what would be done and change nothing (the default)")
	name := fs.String("name", "Kourt court", "the template's name")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *guildID == "" {
		return fmt.Errorf("build-template needs --guild")
	}
	if *dry && *apply {
		return fmt.Errorf("--dry-run and --apply contradict each other")
	}

	// One layout value, validated and then applied. Calling CourtLayout() twice
	// was harmless — it is pure — but it read as though the thing checked and the
	// thing sent could differ, which is the one property this dry run exists to
	// promise.
	layout := guild.CourtLayout()
	ops, errs := guild.Plan(layout)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  invalid: %v\n", e)
		}
		return fmt.Errorf("refusing to touch guild %s: the layout has %d problem(s)", *guildID, len(errs))
	}
	for _, op := range ops {
		fmt.Println(op)
	}
	fmt.Printf("\ntemplate %q on guild %s\n", *name, *guildID)

	if !*apply {
		fmt.Printf("\ndry run — nothing was sent. Re-run with --apply to do it.\n")
		return nil
	}

	token := strings.TrimSpace(os.Getenv(tokenEnv))
	if token == "" {
		return fmt.Errorf("--apply needs a bot token in $%s", tokenEnv)
	}
	c := discordapi.New(token)
	if err := c.ApplyLayout(*guildID, layout, func(kind, name, id string) {
		fmt.Printf("created %-8s %-16q %s\n", kind, name, id)
	}); err != nil {
		return err
	}

	code, synced, err := c.EnsureTemplate(*guildID, *name, "A Kourt court server.")
	if err != nil {
		return err
	}
	link, err := guild.TemplateURL(code)
	if err != nil {
		return fmt.Errorf("Discord returned template code %q, which is not a shape we can "+
			"build a link from: %w", code, err)
	}
	if synced {
		fmt.Printf("\nsynced the guild's existing template\n")
	}
	fmt.Printf("\ntemplate %s\n%s\n", code, link)
	return nil
}
