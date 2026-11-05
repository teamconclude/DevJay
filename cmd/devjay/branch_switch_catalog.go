package main

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

type switchRemoteMapping struct {
	remote, source, destination string
	negative                    bool
}

func readSwitchRemoteMappings(ctx context.Context, path string) ([]switchRemoteMapping, error) {
	out, err := git(ctx, path, "remote")
	if err != nil {
		return nil, errors.New("Could not list configured remotes.")
	}
	var mappings []switchRemoteMapping
	for _, remote := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if remote == "" {
			continue
		}
		values, err := git(ctx, path, "config", "--get-all", "remote."+remote+".fetch")
		if err != nil && !exitedWith(err, 1) {
			return nil, errors.New("Could not read remote branch mappings.")
		}
		for _, spec := range strings.Split(strings.TrimSuffix(string(values), "\n"), "\n") {
			spec = strings.TrimPrefix(spec, "+")
			if strings.HasPrefix(spec, "^") {
				mappings = append(mappings, switchRemoteMapping{remote: remote, source: strings.TrimPrefix(spec, "^"), negative: true})
				continue
			}
			source, destination, ok := strings.Cut(spec, ":")
			if !ok || !strings.HasPrefix(source, "refs/heads/") || !strings.HasPrefix(destination, "refs/remotes/") {
				continue
			}
			if strings.Count(source, "*") != strings.Count(destination, "*") || strings.Count(source, "*") > 1 {
				continue
			}
			mappings = append(mappings, switchRemoteMapping{remote: remote, source: source, destination: destination})
		}
	}
	return mappings, nil
}

func mapSwitchRef(ref, pattern, replacement string) (string, bool) {
	before, after, wildcard := strings.Cut(pattern, "*")
	if !wildcard {
		return replacement, ref == pattern
	}
	if !strings.HasPrefix(ref, before) || !strings.HasSuffix(ref, after) || len(ref) < len(before)+len(after) {
		return "", false
	}
	value := ref[len(before) : len(ref)-len(after)]
	return strings.Replace(replacement, "*", value, 1), true
}

func excludedSwitchRef(mappings []switchRemoteMapping, remote, source string) bool {
	for _, mapping := range mappings {
		if mapping.remote == remote && mapping.negative {
			if _, matches := mapSwitchRef(source, mapping.source, ""); matches {
				return true
			}
		}
	}
	return false
}

// Resolve configured refspecs instead of splitting origin/branch at a slash.
// Ambiguous mappings are rejected; Git's tracking setup must identify one remote.
func switchTrackingRef(ctx context.Context, path, remote, branch string) (string, error) {
	mappings, err := readSwitchRemoteMappings(ctx, path)
	if err != nil {
		return "", err
	}
	return mappedSwitchTrackingRef(mappings, remote, branch)
}

func mappedSwitchTrackingRef(mappings []switchRemoteMapping, remote, branch string) (string, error) {
	source := "refs/heads/" + branch
	if excludedSwitchRef(mappings, remote, source) {
		return "", errors.New("This branch is excluded by the remote fetch configuration.")
	}
	destinations := map[string]bool{}
	for _, mapping := range mappings {
		if mapping.remote == remote && !mapping.negative {
			if destination, matches := mapSwitchRef(source, mapping.source, mapping.destination); matches {
				destinations[destination] = true
			}
		}
	}
	if len(destinations) != 1 {
		return "", errors.New("The selected remote has no unambiguous tracking ref for this branch. Handle it manually.")
	}
	var destination string
	for value := range destinations {
		destination = value
	}
	owners := map[string]bool{}
	for _, mapping := range mappings {
		if mapping.negative {
			continue
		}
		if mappedSource, matches := mapSwitchRef(destination, mapping.destination, mapping.source); matches && !excludedSwitchRef(mappings, mapping.remote, mappedSource) {
			owners[mapping.remote+"\x00"+mappedSource] = true
		}
	}
	if len(owners) != 1 || !owners[remote+"\x00"+source] {
		return "", errors.New("More than one remote maps to this tracking ref. Handle it manually.")
	}
	return destination, nil
}

func readSwitchBranches(ctx context.Context, path string) (BranchCatalog, error) {
	_, meta := inspectLocal(ctx, path)
	if meta.ReadError != nil || meta.Initial || meta.Branch == "(detached)" || meta.OID == "" {
		return BranchCatalog{}, errors.New("Could not read the current checked-out branch. Refresh the Site and try again.")
	}
	result := BranchCatalog{Branch: meta.Branch, Head: meta.OID, Branches: []BranchChoice{}, Message: "Local and previously fetched remote branches. Checking the remotes for new branches…"}
	mappings, err := readSwitchRemoteMappings(ctx, path)
	if err != nil {
		return result, err
	}
	out, err := git(ctx, path, "for-each-ref", "--count=1001", "--format=%(refname)%00%(symref)%00%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/", "refs/remotes/")
	if err != nil {
		return result, errors.New("Could not read the branch list.")
	}
	local := map[string]bool{}
	var remoteRefs []string
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	for _, line := range lines {
		fields := strings.Split(line, "\x00")
		if len(fields) != 4 || fields[1] != "" {
			continue
		}
		ref := fields[0]
		if strings.HasPrefix(ref, "refs/heads/") {
			name := strings.TrimPrefix(ref, "refs/heads/")
			if name == "@" && name != meta.Branch {
				continue
			}
			local[name] = true
			result.Branches = append(result.Branches, BranchChoice{Name: name, Local: true, Current: name == meta.Branch, prRemote: fields[2], prBranch: trackedPullRequestBranch(fields[2], fields[3])})
		} else if strings.HasPrefix(ref, "refs/remotes/") {
			remoteRefs = append(remoteRefs, ref)
		}
	}
	if !local[meta.Branch] {
		local[meta.Branch] = true
		result.Branches = append(result.Branches, BranchChoice{Name: meta.Branch, Local: true, Current: true})
	}
	seen := map[string]bool{}
	localRemotes := map[string][]BranchChoice{}
	for _, ref := range remoteRefs {
		choices := map[string]BranchChoice{}
		for _, mapping := range mappings {
			if mapping.negative {
				continue
			}
			source, matches := mapSwitchRef(ref, mapping.destination, mapping.source)
			if !matches || excludedSwitchRef(mappings, mapping.remote, source) {
				continue
			}
			name := strings.TrimPrefix(source, "refs/heads/")
			if name == "" || name == "HEAD" || name == "@" || strings.HasPrefix(name, "-") {
				continue
			}
			choices[mapping.remote+"\x00"+name] = BranchChoice{Name: name, Remote: mapping.remote, prRemote: mapping.remote, prBranch: name}
		}
		if len(choices) != 1 {
			continue
		}
		for key, choice := range choices {
			if local[choice.Name] {
				localRemotes[choice.Name] = append(localRemotes[choice.Name], choice)
			}
			if !local[choice.Name] && !seen[key] {
				result.Branches = append(result.Branches, choice)
				seen[key] = true
			}
		}
	}
	for i := range result.Branches {
		choice := &result.Branches[i]
		if matches := localRemotes[choice.Name]; choice.Local && choice.prRemote == "" && len(matches) == 1 {
			choice.prRemote, choice.prBranch = matches[0].prRemote, matches[0].prBranch
		}
	}
	sortSwitchBranches(result.Branches)
	if len(lines) > 1000 || len(result.Branches) > 1000 {
		if len(result.Branches) > 1000 {
			result.Branches = result.Branches[:1000]
		}
		result.Message += " The list is limited to the first 1,000 refs; use Git for other branches."
	}
	return result, nil
}

func sortSwitchBranches(branches []BranchChoice) {
	sort.Slice(branches, func(i, j int) bool {
		a, b := branches[i], branches[j]
		if a.Current != b.Current {
			return a.Current
		}
		if a.Local != b.Local {
			return a.Local
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Remote < b.Remote
	})
}

// Discover advertised branches without fetching objects or changing any refs.
// Keep the initial choices usable while this runs, and retain cached choices
// for unavailable remotes. Preparation still checks and fetches the selected tip.
func discoverSwitchBranches(ctx context.Context, path string, catalog BranchCatalog) BranchCatalog {
	result := catalog
	result.Branches = nil
	mappings, err := readSwitchRemoteMappings(ctx, path)
	if err != nil {
		result.Branches = append([]BranchChoice(nil), catalog.Branches...)
		result.Message = "Could not check remote branches. Showing local and previously fetched branches."
		return result
	}
	checked, attempted := map[string]bool{}, map[string]bool{}
	discovered := map[string]BranchChoice{}
	var unavailable []string
	limited := strings.Contains(catalog.Message, "limited")
	for _, mapping := range mappings {
		remote := mapping.remote
		if mapping.negative || attempted[remote] {
			continue
		}
		if len(attempted) >= 4 {
			limited = true
			break
		}
		attempted[remote] = true
		out, err := networkGit(ctx, remoteCommandSlots, 8*time.Second, path, "ls-remote", "--heads", "--refs", "--", remote)
		if err != nil {
			unavailable = append(unavailable, remote)
			continue
		}
		checked[remote] = true
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			_, ref, ok := strings.Cut(line, "\t")
			name, head := strings.CutPrefix(ref, "refs/heads/")
			if !ok || !head || name == "" || name == "HEAD" || name == "@" || strings.HasPrefix(name, "-") {
				continue
			}
			if _, err := mappedSwitchTrackingRef(mappings, remote, name); err != nil {
				continue
			}
			if len(discovered) >= 1000 {
				limited = true
				break
			}
			discovered[remote+"\x00"+name] = BranchChoice{Name: name, Remote: remote, prRemote: remote, prBranch: name}
		}
	}
	local := map[string]bool{}
	for _, choice := range catalog.Branches {
		if choice.Local {
			local[choice.Name] = true
			result.Branches = append(result.Branches, choice)
		} else if !checked[choice.Remote] {
			discovered[choice.Remote+"\x00"+choice.Name] = choice
		}
	}
	localRemotes := map[string][]BranchChoice{}
	for _, choice := range discovered {
		if local[choice.Name] {
			localRemotes[choice.Name] = append(localRemotes[choice.Name], choice)
		} else {
			result.Branches = append(result.Branches, choice)
		}
	}
	for i := range result.Branches {
		choice := &result.Branches[i]
		if matches := localRemotes[choice.Name]; choice.Local && choice.prRemote == "" && len(matches) == 1 {
			choice.prRemote, choice.prBranch = matches[0].prRemote, matches[0].prBranch
		}
	}
	sortSwitchBranches(result.Branches)
	result.Message = "Local and remote branches. Selecting a remote branch prepares a local tracking branch; switching requires confirmation."
	if len(unavailable) > 0 {
		result.Message += " Could not check " + strings.Join(unavailable, ", ") + "; showing previously fetched branches for those remotes."
	}
	if len(result.Branches) > 1000 {
		result.Branches = result.Branches[:1000]
		limited = true
	}
	if limited {
		result.Message += " Discovery is limited to four remotes and 1,000 branches; use Git for other branches."
	}
	return result
}
