//go:build !windows

package main

func guiSmoke(*protocol) object { panic("Native GUI validation requires Windows") }
