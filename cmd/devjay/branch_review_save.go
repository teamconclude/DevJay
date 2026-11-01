package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"
)

func saveBranchReviewFile(ctx context.Context, v *GitView, expectedToken, content string) error {
	if v.Review == nil || v.Review.Selected == nil || !v.Review.Selected.Editable || expectedToken == "" || expectedToken != v.Review.Selected.Token {
		return errors.New("Refresh the file before saving.")
	}
	if len(content) > branchReviewFileLimit || !utf8.ValidString(content) || bytes.IndexByte([]byte(content), 0) >= 0 {
		return errors.New("Only UTF-8 text files up to 256 KiB can be saved.")
	}
	if err := branchReviewIdentity(ctx, v); err != nil {
		return err
	}
	common := gitString(ctx, v.root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if common == "" {
		return errors.New("Could not locate the repository.")
	}
	lock, err := os.OpenFile(filepath.Join(common, "devjay-action.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("Another DevJay action is running for this repository.")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	selected := v.Review.Selected
	path := selected.Path
	if !branchReviewSafePath(path) {
		return errors.New("This path cannot be edited safely.")
	}
	if err := branchReviewParentsRegular(v.root, path); err != nil {
		return err
	}
	validate := func() error {
		data, info, err := readBranchReviewWorktree(v.root, path)
		if err != nil || info == nil || !info.Mode().IsRegular() || !os.SameFile(info, selected.info) || branchReviewToken(v, path, data, info.Mode()) != expectedToken {
			return errors.New("This file changed since you opened it. Your draft is kept; copy it before refreshing.")
		}
		return nil
	}
	if err := validate(); err != nil {
		return err
	}
	if err := branchReviewIdentity(ctx, v); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// A confirmed Save is one operation: cancellation cannot interrupt a partial
	// write or turn a completed replacement into an apparent cancellation.
	ctx = context.WithoutCancel(ctx)
	root, err := os.OpenRoot(v.root)
	if err != nil {
		return err
	}
	defer root.Close()
	parent, err := root.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	name := fmt.Sprintf(".devjay-save-%x", nonce)
	tmp, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer parent.Remove(name)
	if _, err = tmp.WriteString(content); err == nil {
		err = tmp.Chmod(selected.info.Mode().Perm())
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// Check the actual destination again immediately before the atomic replacement.
	if err := branchReviewParentsRegular(v.root, path); err != nil {
		return err
	}
	if err := validate(); err != nil {
		return err
	}
	if err := branchReviewIdentity(ctx, v); err != nil {
		return err
	}
	openedParent, parentErr := parent.Stat(".")
	currentParent, currentErr := root.Stat(filepath.Dir(path))
	if parentErr != nil || currentErr != nil || !os.SameFile(openedParent, currentParent) {
		return errors.New("The containing folder changed. Refresh before saving.")
	}
	if err := parent.Rename(name, filepath.Base(path)); err != nil {
		return err
	}
	base, ignore := v.Review.BaseRef, v.Review.IgnoreWhitespace
	if err := loadBranchReview(ctx, v, base, ignore); err != nil {
		next := *v.Review
		next.Note = "Saved the file, but the review could not refresh: " + err.Error()
		v.Review = &next
		return nil
	}
	next := *v.Review
	next.Note = "Saved to the working tree. The index and commits were not changed."
	v.Review = &next
	return nil
}
