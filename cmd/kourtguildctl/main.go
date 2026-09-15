// Command kourtguildctl is the operator's side of the Kourt ↔ Discord bridge.
//
// It prints the layout a court server gets, the permission set the bot asks for,
// and the two links a court moderator clicks — and it can apply that layout to a
// reference guild and mint the template every other server is created from.
//
//	kourtguildctl plan                       # the layout, as calls, printed
//	kourtguildctl perms                      # the permission set and its integer
//	kourtguildctl mod --court meta --addr g1…  # may this address publish it?
//	kourtguildctl links --court meta ...     # the two links, for one court
//	kourtguildctl claim --court meta ...     # sign for it, and publish
//	kourtguildctl build-template --guild ID  # dry run: prints, changes nothing
//	kourtguildctl build-template --guild ID --apply
//
// `mod` is the one to run first. Everything else is preparation for a server that
// only a court's moderators can ever publish, so an address that is not one is a
// wasted afternoon.
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
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gnolang/gno/tm2/pkg/crypto/keys"
	"github.com/jaekwon/kourt/internal/binding"
	"github.com/jaekwon/kourt/internal/discordapi"
	"github.com/jaekwon/kourt/internal/gnorpc"
	"github.com/jaekwon/kourt/internal/guild"
	"golang.org/x/term"
)

const tokenEnv = "KOURT_DISCORD_TOKEN"

// THE DEFAULTS ARE THE SITE'S, NOT THE OTHER CHAIN'S.
//
// These pointed at rpc.kourt.xyz and the kourtv2 realm, which is a real chain
// with real courts and NOT the one kourt.xyz reads — the deploy stamps
// gnoland-1 and the unit passes the realm below. So `mod` answered confidently
// about a court set the site never looks at, and the answer was wrong in the
// most convincing possible way: a court that exists, a moderator that checks out,
// on a chain nobody is serving.
const (
	DefaultChain   = "gnoland-1"
	DefaultRPC     = "https://rpc.gno.land"
	DefaultRealm   = "gno.land/r/g1ecsuj0q572jr0dhu29q9njtnmw03hyu7tyyvv6/kourt"
	DefaultService = "https://kourt.xyz"
)

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
	case "mod":
		err = cmdMod(args)
	case "claim":
		err = cmdClaim(args)
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
  mod                     ask the chain whether an address may publish a court's server
  claim                   sign for a server and publish it as that court's own
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
	chain := fs.String("chain", DefaultChain, "chain id the court lives on")
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

// cmdMod answers the question everything else depends on, before anybody spends
// time on a server that will never be publishable.
//
// It is a read against a live node, so it is the one verb here whose answer can
// change without this repo changing — which is exactly why it is a command rather
// than something written down.
func cmdMod(argv []string) error {
	fs := flag.NewFlagSet("mod", flag.ContinueOnError)
	court := fs.String("court", "", "court slug, as the chain spells it")
	addr := fs.String("addr", "", "the gno address to ask about")
	rpc := fs.String("rpc", DefaultRPC, "the node to ask")
	pkg := fs.String("pkg", DefaultRealm, "the realm holding the courts")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *court == "" || *addr == "" {
		return fmt.Errorf("mod needs --court and --addr")
	}

	v := &binding.Verifier{
		Node:    &gnorpc.Node{RPC: *rpc, ID: "kourtguildctl"},
		PkgPath: *pkg,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	isMod, err := v.IsMod(ctx, *court, *addr)
	if errors.Is(err, binding.ErrNoCourt) {
		// The realm's panic arrives wrapped in a multi-line gno traceback. That
		// is the right thing in a log and the wrong thing here: an operator who
		// mistyped a slug should read one line, not a stack.
		return fmt.Errorf("no court %q on %s — check the slug, or the --rpc/--pkg you pointed at", *court, *rpc)
	}
	if err != nil {
		return err
	}
	if isMod {
		fmt.Printf("yes — %s may publish %s's Discord server\n", *addr, *court)
	} else {
		fmt.Printf("no — %s is not a moderator of %s, so it cannot publish that\n"+
			"court's server. Whoever created the court can appoint it with\n"+
			"AppointMods, or sign with a key that is already a moderator.\n", *addr, *court)
	}

	// The threshold is reported either way: it is what a moderator needs to know
	// next, and on a court that has never appointed anybody it says 1-of-1, which
	// is the honest answer about how much a single signature means there.
	m, n, err := v.ModThreshold(ctx, *court)
	if err != nil {
		// Not fatal. The first answer is the one that was asked for, and a
		// threshold that did not read should not make it look unanswered.
		fmt.Fprintf(os.Stderr, "(the court's m-of-n did not read: %v)\n", err)
		return nil
	}
	fmt.Printf("%s's moderator threshold is %d-of-%d\n", *court, m, n)
	return nil
}

// cmdClaim is the step the browser cannot take.
//
// PUBLISHING NEEDS A SIGNATURE, and the overlay has no way to make one: Adena is
// wired into the page for TRANSACTIONS — AddEstablish, DoContract, GetAccount —
// and an arbitrary-message signature is a different call it does not offer. So
// the flow was complete at both ends and joined in the middle by nothing: a
// moderator could add the bot from the court page and then had no way at all to
// publish the server.
//
// This closes it from the side that needs no wallet support. It reads the same
// keybase gnokey uses, signs the same challenge the service builds, and posts it.
//
// THE PASSPHRASE IS READ FROM THE TERMINAL, never a flag. A key that can publish
// a court's server is a key worth the same care as the rest of this tool gives a
// bot token, and a passphrase in argv is in the process table.
func cmdClaim(argv []string) error {
	fs := flag.NewFlagSet("claim", flag.ContinueOnError)
	court := fs.String("court", "", "court slug, as the chain spells it")
	chain := fs.String("chain", DefaultChain, "chain id the court lives on")
	guildID := fs.String("guild", "", "the Discord server to publish")
	nonce := fs.String("nonce", "", "reuse a nonce instead of starting a fresh flow")
	key := fs.String("key", "", "name or address of the signing key in the keybase")
	home := fs.String("home", "", "keybase directory (default: gnokey's)")
	service := fs.String("service", DefaultService, "the Kourt service to post the claim to")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *court == "" || *guildID == "" || *key == "" {
		return fmt.Errorf("claim needs --court, --guild and --key")
	}

	// THE NONCE IS FETCHED, NOT PASTED.
	//
	// It exists to tie a returning redirect to the browser that started the flow,
	// and at a terminal there is no browser and no redirect — so making an
	// operator run `links`, copy a 48-character hex string out of a URL and paste
	// it into a second command bought exactly nothing and cost the one step in
	// this whole sequence people will get wrong. Starting the flow here keeps the
	// nonce server-minted and single-use, which is the property that matters,
	// while removing the copy.
	//
	// --nonce still overrides, for the case where a flow was started in a browser
	// and is being finished at a terminal.
	if *nonce == "" {
		n, err := startFlow(*service, *chain, *court)
		if err != nil {
			return fmt.Errorf("starting a flow with %s: %w", *service, err)
		}
		*nonce = n
	}

	dir := *home
	if dir == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(h, ".gnokey")
	}
	kb, err := keys.NewKeyBaseFromDir(dir)
	if err != nil {
		return fmt.Errorf("opening the keybase at %s: %w", dir, err)
	}
	info, err := kb.GetByNameOrAddress(*key)
	if err != nil {
		return fmt.Errorf("no key %q in %s: %w", *key, dir, err)
	}
	addr := info.GetAddress().String()

	// THE CHALLENGE IS BUILT FROM THE SAME FUNCTION THE SERVICE USES. Two
	// spellings of what is being signed is the one mistake that would make every
	// honest claim fail with "that signature does not check out".
	st := guild.State{Chain: *chain, Court: *court, Nonce: *nonce}
	challenge, err := guild.ChallengeText(st, *guildID)
	if err != nil {
		return err
	}

	// SHOWN BEFORE IT IS SIGNED. This is what a wallet's prompt would do, and the
	// reason the challenge is plain text rather than a hash: somebody about to
	// publish an outbound link under a court's name should read what they are
	// agreeing to.
	fmt.Printf("About to sign, as %s:\n\n%s\n\n", addr, challenge)

	pass, err := readPassphrase("passphrase for " + *key + ": ")
	if err != nil {
		return err
	}
	sig, pub, err := kb.Sign(*key, pass, []byte(challenge))
	if err != nil {
		return fmt.Errorf("signing: %w", err)
	}

	body, err := json.Marshal(binding.ClaimRequest{
		Chain: *chain, Court: *court, GuildID: *guildID, Nonce: *nonce,
		Address:   addr,
		PubKey:    base64.StdEncoding.EncodeToString(pub.Bytes()),
		Signature: base64.StdEncoding.EncodeToString(sig),
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(*service, "/")+"/api/guild/claim", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// The service's own sentence, which is written for a person: it says which
		// of the three checks refused and what to do about it.
		return fmt.Errorf("the service refused the claim (%s): %s",
			resp.Status, strings.TrimSpace(string(out)))
	}
	fmt.Printf("published: %s\n", strings.TrimSpace(string(out)))
	return nil
}

// startFlow asks the service for a nonce, the same call the court page makes.
func startFlow(service, chain, court string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	u := strings.TrimSuffix(service, "/") + "/api/guild/start?chain=" +
		url.QueryEscape(chain) + "&court=" + url.QueryEscape(court)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var out struct {
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Nonce == "" {
		// The likeliest cause by far, and it looks like a parser bug otherwise.
		return "", fmt.Errorf("no nonce in the reply — if this is HTML, nginx has no "+
			"/api/guild/ location and the app is answering instead: %.120s", b)
	}
	return out.Nonce, nil
}

// readPassphrase reads without echoing when there is a terminal, and from stdin
// when there is not — so a script can pipe one in without the prompt hanging.
func readPassphrase(prompt string) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
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
