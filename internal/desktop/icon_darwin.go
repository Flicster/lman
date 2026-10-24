//go:build darwin

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void setDockIcon(const void *bytes, int length) {
	NSData *data = [NSData dataWithBytes:bytes length:(NSUInteger)length];
	NSImage *image = [[NSImage alloc] initWithData:data];
	if (image == nil) {
		return;
	}
	[[NSApplication sharedApplication] setApplicationIconImage:image];
}
*/
import "C"
import "unsafe"

// applyAppIcon sets the Dock icon. A plain binary has no bundle icon of its own.
func applyAppIcon(png []byte) {
	if len(png) == 0 {
		return
	}
	C.setDockIcon(unsafe.Pointer(&png[0]), C.int(len(png)))
}
