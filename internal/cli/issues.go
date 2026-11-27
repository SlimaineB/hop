package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/providers"
	"github.com/SlimaineB/hop/internal/providers/builtin"
	"github.com/SlimaineB/hop/internal/servers"
)

var regexpTicketKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-[0-9]+$`)

func (a *App) newIssueCommand() *cobra.Command {
	var printURL bool
	cmd := &cobra.Command{
		Use:   "issue [key|terms...]",
		Short: "Open, search or create tickets",
		Long: `Open, search or create tickets in configured providers.

A ticket key opens directly:
  hop issue OPS-123

Free-form terms search every configured provider:
  hop issue "checkout timeout"

If several providers can handle the request, hop prompts a menu. Provider
aliases also work without a flag:
  hop jira OPS-123
  hop jira search "checkout timeout"
  hop jira new "Pod api crashloop"`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			set, err := loadProviderSet(cfg)
			if err != nil {
				return err
			}
			return a.runIssue(cmd, cfg, set, "", args, printURL)
		},
	}
	cmd.PersistentFlags().BoolVar(&printURL, "print", false, "print the URL instead of opening the browser")
	cmd.AddCommand(
		a.newIssueSearchCommand(),
		a.newIssueNewCommand(),
		a.newIssueOpenCommand(),
		a.newIssueProvidersCommand(),
	)
	return cmd
}

func (a *App) newIssueSearchCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "search [terms...]",
		Short: "Search tickets across providers",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			set, err := loadProviderSet(cfg)
			if err != nil {
				return err
			}
			return a.searchTickets(cmd, set.All(), strings.Join(args, " "), limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum tickets per provider")
	return cmd
}

func (a *App) newIssueOpenCommand() *cobra.Command {
	var printURL bool
	cmd := &cobra.Command{
		Use:   "open KEY",
		Short: "Open one ticket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			set, err := loadProviderSet(cfg)
			if err != nil {
				return err
			}
			return a.openTicketInput(cmd, set, "", args[0], printURL)
		},
	}
	cmd.Flags().BoolVar(&printURL, "print", false, "print the URL instead of opening the browser")
	return cmd
}

func (a *App) newIssueNewCommand() *cobra.Command {
	var (
		title     string
		body      string
		bodyFile  string
		project   string
		issueType string
		fromPod   string
		fromVM    string
		openAfter bool
	)
	cmd := &cobra.Command{
		Use:   "new [title...]",
		Short: "Create a ticket",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			set, err := loadProviderSet(cfg)
			if err != nil {
				return err
			}
			if title == "" {
				title = strings.Join(args, " ")
			}
			if bodyFile != "" {
				data, err := readBodyFile(cmd, bodyFile)
				if err != nil {
					return err
				}
				body = strings.TrimSpace(body + "\n\n" + string(data))
			}
			if fromPod != "" {
				podTitle, podBody, err := a.podIssueContext(cmd, cfg, fromPod)
				if err != nil {
					return err
				}
				if title == "" {
					title = podTitle
				}
				body = strings.TrimSpace(body + "\n\n" + podBody)
			}
			if fromVM != "" {
				vmTitle, vmBody, err := a.vmIssueContext(cfg, fromVM)
				if err != nil {
					return err
				}
				if title == "" {
					title = vmTitle
				}
				body = strings.TrimSpace(body + "\n\n" + vmBody)
			}
			provider, err := selectProvider(cmd, set, cfg.Issues.DefaultProvider, "", "create a ticket")
			if err != nil {
				return err
			}
			return a.createTicket(cmd, provider, title, body, project, issueType, openAfter)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "ticket title")
	cmd.Flags().StringVar(&body, "body", "", "ticket description")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "read description from a file, or - for stdin")
	cmd.Flags().StringVar(&project, "project", "", "provider project override")
	cmd.Flags().StringVar(&issueType, "type", "", "provider issue type override")
	cmd.Flags().StringVar(&fromPod, "from-pod", "", "prefill from a pod name")
	cmd.Flags().StringVar(&fromVM, "from-vm", "", "prefill from a VM/server name")
	cmd.Flags().BoolVar(&openAfter, "open", false, "open the ticket after creation")
	return cmd
}

func (a *App) newIssueProvidersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "providers",
		Short: "List configured issue providers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			return printProviders(cmd, cfg)
		},
	}
}

func (a *App) runIssue(cmd *cobra.Command, cfg *config.Config, set *providers.Set, forced string, args []string, printURL bool) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	if len(args) == 1 {
		candidates := providerList(set, forced)
		matches := detectProviders(candidates, args[0])
		if forced != "" {
			matches = nil
			for _, provider := range candidates {
				if _, ok := normalizedTicketKey(provider, args[0]); ok {
					matches = candidates
					break
				}
			}
		}
		if len(matches) > 0 {
			provider, err := chooseProvider(cmd, matches, "open "+args[0])
			if err != nil {
				return err
			}
			key, ok := normalizedTicketKey(provider, args[0])
			if !ok {
				return fmt.Errorf("provider %s does not recognize ticket key %s", provider.ID(), args[0])
			}
			return a.openTicket(cmd, provider, key, printURL)
		}
		if ticketKeyLike(args[0]) {
			return fmt.Errorf("no configured provider recognizes ticket key %s", args[0])
		}
	}
	return a.searchTickets(cmd, providerList(set, forced), strings.Join(args, " "), 10)
}

func (a *App) runProviderAlias(cmd *cobra.Command, cfg *config.Config, id string, args []string, printURL bool) error {
	set, err := loadProviderSet(cfg)
	if err != nil {
		return err
	}
	provider, ok := set.Get(id)
	if !ok {
		return fmt.Errorf("provider %q is not configured", id)
	}
	if len(args) == 0 {
		return a.searchTickets(cmd, []providers.TicketProvider{provider}, "", 10)
	}
	switch strings.ToLower(args[0]) {
	case "new":
		title := strings.Join(args[1:], " ")
		if title == "" {
			return fmt.Errorf("usage: hop %s new TITLE", id)
		}
		return a.createTicket(cmd, provider, title, "", "", "", false)
	case "search":
		return a.searchTickets(cmd, []providers.TicketProvider{provider}, strings.Join(args[1:], " "), 10)
	case "open":
		if len(args) != 2 {
			return fmt.Errorf("usage: hop %s open KEY", id)
		}
		return a.openTicketInput(cmd, set, id, args[1], printURL)
	case "providers":
		return printProviders(cmd, cfg)
	default:
		return a.runIssue(cmd, cfg, set, id, args, printURL)
	}
}

func (a *App) openTicketInput(cmd *cobra.Command, set *providers.Set, forced, input string, printURL bool) error {
	candidates := providerList(set, forced)
	matches := detectProviders(candidates, input)
	if forced != "" {
		matches = candidates
	}
	if len(matches) == 0 {
		return fmt.Errorf("no configured provider recognizes ticket key %s", input)
	}
	provider, err := chooseProvider(cmd, matches, "open "+input)
	if err != nil {
		return err
	}
	key, ok := normalizedTicketKey(provider, input)
	if !ok {
		return fmt.Errorf("provider %s does not recognize ticket key %s", provider.ID(), input)
	}
	return a.openTicket(cmd, provider, key, printURL)
}

func (a *App) openTicket(cmd *cobra.Command, provider providers.TicketProvider, key string, printURL bool) error {
	target := provider.URL(key)
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("provider %s returned an invalid URL: %s", provider.ID(), target)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", key, target)
	if printURL {
		return nil
	}
	return actions.OpenBrowser(target)
}

func (a *App) searchTickets(cmd *cobra.Command, list []providers.TicketProvider, query string, limit int) error {
	if len(list) == 0 {
		return fmt.Errorf("no issue providers configured; add a providers block to the hop config")
	}
	if limit <= 0 {
		limit = 10
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	var tickets []providers.Ticket
	var failures []string
	for _, provider := range list {
		found, err := provider.Search(ctx, query, limit)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", provider.ID(), err))
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", failures[len(failures)-1])
			continue
		}
		tickets = append(tickets, found...)
	}
	if len(tickets) == 0 {
		if len(failures) > 0 {
			return fmt.Errorf("%s", strings.Join(failures, "; "))
		}
		fmt.Fprintln(cmd.OutOrStdout(), "no tickets found")
		return nil
	}
	sort.SliceStable(tickets, func(i, j int) bool {
		if tickets[i].Provider != tickets[j].Provider {
			return tickets[i].Provider < tickets[j].Provider
		}
		return tickets[i].Key < tickets[j].Key
	})
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "PROVIDER\tKEY\tSTATUS\tTITLE")
	for _, ticket := range tickets {
		status := ticket.Status
		if status == "" {
			status = "-"
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", ticket.Provider, ticket.Key, status, ticket.Title)
	}
	return writer.Flush()
}

func (a *App) createTicket(cmd *cobra.Command, provider providers.TicketProvider, title, body, project, issueType string, openAfter bool) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("ticket title is required")
	}
	fields := map[string]string{}
	if project != "" {
		fields["project"] = project
	}
	if issueType != "" {
		fields["type"] = issueType
	}
	ticket, err := provider.Create(cmd.Context(), providers.TicketDraft{
		Title:  title,
		Body:   body,
		Fields: fields,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "created %s: %s\n%s\n", ticket.Key, ticket.Title, ticket.URL)
	if openAfter {
		return actions.OpenBrowser(ticket.URL)
	}
	return nil
}

func loadProviderSet(cfg *config.Config) (*providers.Set, error) {
	return builtin.Registry().Load(cfg)
}

func providerList(set *providers.Set, forced string) []providers.TicketProvider {
	if forced != "" {
		if provider, ok := set.Get(forced); ok {
			return []providers.TicketProvider{provider}
		}
		return nil
	}
	return set.All()
}

func detectProviders(list []providers.TicketProvider, value string) []providers.TicketProvider {
	var matches []providers.TicketProvider
	for _, provider := range list {
		if _, ok := normalizedTicketKey(provider, value); ok {
			matches = append(matches, provider)
		}
	}
	return matches
}

func normalizedTicketKey(provider providers.TicketProvider, value string) (string, bool) {
	if normalizer, ok := provider.(providers.KeyNormalizer); ok {
		return normalizer.NormalizeKey(value)
	}
	if provider.IsKey(value) {
		return strings.TrimSpace(value), true
	}
	return "", false
}

func ticketKeyLike(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.ContainsAny(value, "#") || regexpTicketKey.MatchString(value) {
		return true
	}
	return strings.Contains(value, "/-/issues/") || strings.Contains(value, "/browse/")
}

func selectProvider(cmd *cobra.Command, set *providers.Set, defaultID, forced, action string) (providers.TicketProvider, error) {
	if forced != "" {
		provider, ok := set.Get(forced)
		if !ok {
			return nil, fmt.Errorf("provider %q is not configured", forced)
		}
		return provider, nil
	}
	if defaultID != "" {
		provider, ok := set.Get(defaultID)
		if !ok {
			return nil, fmt.Errorf("default provider %q is not configured", defaultID)
		}
		return provider, nil
	}
	if len(set.All()) == 0 {
		return nil, fmt.Errorf("no issue providers configured")
	}
	if len(set.All()) == 1 {
		return set.All()[0], nil
	}
	return chooseProvider(cmd, set.All(), action)
}

func chooseProvider(cmd *cobra.Command, list []providers.TicketProvider, action string) (providers.TicketProvider, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("no provider available")
	}
	if len(list) == 1 {
		return list[0], nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, fmt.Errorf("several providers can %s; run hop <provider> ... instead", action)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Several providers can %s:\n", action)
	for i, provider := range list {
		fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s (%s)\n", i+1, provider.ID(), provider.Type())
	}
	fmt.Fprint(cmd.OutOrStdout(), "Choose: ")
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		return nil, fmt.Errorf("no provider selected")
	}
	choice, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || choice < 1 || choice > len(list) {
		return nil, fmt.Errorf("invalid provider choice")
	}
	return list[choice-1], nil
}

func printProviders(cmd *cobra.Command, cfg *config.Config) error {
	if len(cfg.Providers) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no issue providers configured")
		return nil
	}
	ids := make([]string, 0, len(cfg.Providers))
	for id := range cfg.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tTYPE\tKEYWORDS\tDEFAULT\tBASE URL")
	for _, id := range ids {
		provider := cfg.Providers[id]
		keywords := strings.Join(provider.Keywords, ",")
		if keywords == "" {
			keywords = "-"
		}
		def := ""
		if cfg.Issues.DefaultProvider == id {
			def = "yes"
		}
		if def == "" {
			def = "-"
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", id, provider.Type, keywords, def, provider.BaseURL)
	}
	return writer.Flush()
}

func providerAlias(cfg *config.Config, args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	if name == "" {
		return "", false
	}
	if _, ok := cfg.Providers[name]; ok {
		return name, true
	}
	var matches []string
	for id, provider := range cfg.Providers {
		if strings.EqualFold(strings.TrimSpace(provider.Type), name) {
			matches = append(matches, id)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func readBodyFile(cmd *cobra.Command, name string) ([]byte, error) {
	if name == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read body file %s: %w", name, err)
	}
	return data, nil
}

func (a *App) podIssueContext(cmd *cobra.Command, cfg *config.Config, name string) (string, string, error) {
	service, err := app.New(cfg)
	if err != nil {
		return "", "", err
	}
	defer service.Close()
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	result, err := service.Search(ctx, app.SearchRequest{Query: name, Limit: 1})
	if err != nil {
		return "", "", err
	}
	if len(result.Hits) == 0 {
		return "", "", fmt.Errorf("pod not found: %s", name)
	}
	pod := result.Hits[0].Pod
	var body strings.Builder
	fmt.Fprintf(&body, "## Pod\n\n")
	fmt.Fprintf(&body, "- context: `%s`\n", pod.Context)
	fmt.Fprintf(&body, "- cluster: `%s`\n", pod.Cluster)
	fmt.Fprintf(&body, "- namespace: `%s`\n", pod.Namespace)
	fmt.Fprintf(&body, "- name: `%s`\n", pod.Name)
	fmt.Fprintf(&body, "- phase: `%s`\n", pod.Phase)
	if pod.Node != "" {
		fmt.Fprintf(&body, "- node: `%s`\n", pod.Node)
	}
	if pod.OwnerName != "" {
		fmt.Fprintf(&body, "- owner: `%s/%s`\n", pod.OwnerKind, pod.OwnerName)
	}
	if pod.PodIP != "" {
		fmt.Fprintf(&body, "- pod IP: `%s`\n", pod.PodIP)
	}
	if len(pod.Containers) > 0 {
		body.WriteString("\n### Containers\n\n")
		for _, container := range pod.Containers {
			fmt.Fprintf(&body, "- `%s`: `%s`\n", container.Name, container.Image)
		}
	}
	return fmt.Sprintf("Pod %s/%s %s", pod.Namespace, pod.Name, pod.Phase), body.String(), nil
}

func (a *App) vmIssueContext(cfg *config.Config, name string) (string, string, error) {
	service := servers.New(cfg)
	server, err := service.Find(name)
	if err != nil {
		return "", "", err
	}
	sshCommand, _ := service.SSHCommand(server.Name)
	var body strings.Builder
	fmt.Fprintf(&body, "## VM\n\n")
	fmt.Fprintf(&body, "- name: `%s`\n", server.Name)
	if server.Env != "" {
		fmt.Fprintf(&body, "- env: `%s`\n", server.Env)
	}
	if server.Host != "" {
		fmt.Fprintf(&body, "- host: `%s`\n", server.Host)
	}
	if server.User != "" {
		fmt.Fprintf(&body, "- user: `%s`\n", server.User)
	}
	if len(server.Tags) > 0 {
		fmt.Fprintf(&body, "- tags: `%s`\n", strings.Join(server.Tags, ", "))
	}
	if sshCommand != "" {
		fmt.Fprintf(&body, "\n```sh\n%s\n```\n", sshCommand)
	}
	return fmt.Sprintf("VM %s", server.Name), body.String(), nil
}
