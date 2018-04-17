package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"text/template"

	"github.com/Microsoft/azure-vhd-utils/vhdcore"
	"github.com/Microsoft/azure-vhd-utils/vhdcore/block/bitmap"
	"github.com/Microsoft/azure-vhd-utils/vhdcore/footer"
	"github.com/Microsoft/azure-vhd-utils/vhdcore/vhdfile"
	"gopkg.in/urfave/cli.v1"
)

// FixedDiskBlocksInfo type describes general block information of a fixed disk
//
type FixedDiskBlocksInfo struct {
	BlockSize  int64
	BlockCount int64
}

// ExpandableDiskBlocksInfo type describes general block information of a expandable disk
//
type ExpandableDiskBlocksInfo struct {
	BlockDataSize         int64
	BlockBitmapSize       int32
	BlockBitmapPaddedSize int32
	BlockCount            int64
	UsedBlockCount        int64
	EmptyBlockCount       int64
}

func vhdInspectCmdHandler() cli.Command {
	return cli.Command{
		Name:  "inspect",
		Usage: "Commands to inspect local VHD",
		Subcommands: []cli.Command{
			{
				Name:  "header",
				Usage: "Show VHD header",
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "path",
						Usage: "Path to VHD.",
					},
				},
				Action: showVhdHeader,
			},
			{
				Name:  "footer",
				Usage: "Show VHD footer",
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "path",
						Usage: "Path to VHD.",
					},
				},
				Action: showVhdFooter,
			},
			{
				Name:  "bat",
				Usage: "Show a range of VHD Block allocation table (BAT) entries",
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "path",
						Usage: "Path to VHD.",
					},
					cli.StringFlag{
						Name:  "start-range",
						Usage: "Start range.",
					},
					cli.StringFlag{
						Name:  "end-range",
						Usage: "End range.",
					},
					cli.BoolFlag{
						Name:  "skip-empty",
						Usage: "Do not show BAT entries pointing to empty blocks.",
					},
				},
				Action: showVhdBAT,
			},
			{
				Name:  "block",
				Usage: "Inspect VHD blocks",
				Subcommands: []cli.Command{
					{
						Name:  "info",
						Usage: "Show blocks general information",
						Flags: []cli.Flag{
							cli.StringFlag{
								Name:  "path",
								Usage: "Path to VHD.",
							},
						},
						Action: showVhdBlocksInfo,
					},
					{
						Name:  "bitmap",
						Usage: "Show sector bitmap of a expandable disk's block",
						Flags: []cli.Flag{
							cli.StringFlag{
								Name:  "path",
								Usage: "Path to VHD.",
							},
							cli.StringFlag{
								Name:  "block-index",
								Usage: "Index of the block.",
							},
						},
						Action: showVhdBlockBitmap,
					},
				},
			},
		},
	}
}

const headerTempl = `Cookie            : {{.Cookie }}
DataOffset        : {{.DataOffset}}
TableOffset       : {{.TableOffset}}
HeaderVersion     : {{.HeaderVersion}}
MaxTableEntries   : {{.MaxTableEntries}}
BlockSize         : {{.BlockSize}} bytes
CheckSum          : {{.CheckSum}}
ParentUniqueID    : {{.ParentUniqueID}}
ParentTimeStamp   : {{.ParentTimeStamp | printf "%v"}}
Reserved          : {{.Reserved}}
ParentPath        : {{.ParentPath}}
{{range .ParentLocators}}
  PlatformCode               : {{.PlatformCode}}
  PlatformDataSpace          : {{.PlatformDataSpace}}
  PlatformDataLength         : {{.PlatformDataLength}}
  Reserved                   : {{.Reserved}}
  PlatformDataOffset         : {{.PlatformDataOffset}}
  PlatformSpecificFileLocator: {{.PlatformSpecificFileLocator}}
{{end}}

-- Hex dump --

{{.RawData | dump }}`

func showVhdHeader(c *cli.Context) error {
	vhdPath := c.String("path")
	if vhdPath == "" {
		return errors.New("Missing required argument --path")
	}

	vFileFactory := &vhdFile.FileFactory{}
	vFile, err := vFileFactory.Create(vhdPath)
	if err != nil {
		return err
	}

	defer vFileFactory.Dispose(nil)
	if vFile.GetDiskType() == footer.DiskTypeFixed {
		return errors.New("Warn: Only expandable VHDs has header structure, this is a fixed VHD")
	}

	t, err := template.New("root").
		Funcs(template.FuncMap{"dump": hex.Dump}).
		Parse(headerTempl)
	t.Execute(os.Stdout, vFile.Header)

	return nil
}

const footerTempl = `Cookie            : {{.Cookie }}
Features          : {{.Features}}
FileFormatVersion : {{.FileFormatVersion}}
HeaderOffset      : {{.HeaderOffset}}
TimeStamp         : {{.TimeStamp | printf "%v" }}
CreatorApplication: {{.CreatorApplication}}
CreatorVersion    : {{.CreatorVersion}}
CreatorHostOsType : {{.CreatorHostOsType}}
PhysicalSize      : {{.PhysicalSize}} bytes
VirtualSize       : {{.VirtualSize}} bytes
DiskGeometry      : {{.DiskGeometry}}
DiskType          : {{.DiskType}}
CheckSum          : {{.CheckSum}}
UniqueID          : {{.UniqueID}}
SavedState        : {{.SavedState | printf "%v" }}

-- Hex dump --

{{.RawData | dump }}`

func showVhdFooter(c *cli.Context) error {
	vhdPath := c.String("path")
	if vhdPath == "" {
		return errors.New("Missing required argument --path")
	}

	vFileFactory := &vhdFile.FileFactory{}
	vFile, err := vFileFactory.Create(vhdPath)
	if err != nil {
		return err
	}

	defer vFileFactory.Dispose(nil)
	t, err := template.New("root").
		Funcs(template.FuncMap{"dump": hex.Dump}).
		Parse(footerTempl)
	t.Execute(os.Stdout, vFile.Footer)

	return nil
}

const batTempl = `{{range $index, $value := .}} BAT[{{adj $index}}] : {{$value | printf "0x%X"}}
{{end}}`

func showVhdBAT(c *cli.Context) error {
	vhdPath := c.String("path")
	if vhdPath == "" {
		return errors.New("Missing required argument --path")
	}

	startRange := uint32(0)
	var err error
	if c.IsSet("start-range") {
		r, err := strconv.ParseUint(c.String("start-range"), 10, 32)
		if err != nil {
			return fmt.Errorf("invalid index value --start-range: %s", err)
		}
		startRange = uint32(r)
	}

	endRange := uint32(0)
	if c.IsSet("end-range") {
		r, err := strconv.ParseUint(c.String("end-range"), 10, 32)
		if err != nil {
			return fmt.Errorf("invalid index value --end-range: %s", err)
		}
		endRange = uint32(r)
	}

	vFileFactory := &vhdFile.FileFactory{}
	vFile, err := vFileFactory.Create(vhdPath)
	if err != nil {
		return err
	}

	defer vFileFactory.Dispose(nil)
	if vFile.GetDiskType() == footer.DiskTypeFixed {
		return errors.New("Warn: Only expandable VHDs has Block Allocation Table, this is a fixed VHD")
	}

	maxEntries := vFile.BlockAllocationTable.BATEntriesCount
	if !c.IsSet("end-range") {
		endRange = maxEntries - 1
	}

	if startRange > maxEntries || endRange > maxEntries {
		return fmt.Errorf("index out of boundary, this vhd BAT index range is [0, %d]", maxEntries)
	}

	if startRange > endRange {
		return errors.New("invalid range --start-range > --end-range")
	}

	fMap := template.FuncMap{
		"adj": func(i int) int {
			return i + int(startRange)
		},
	}

	t, _ := template.New("root").
		Funcs(fMap).
		Parse(batTempl)

	if !c.IsSet("skip-empty") {
		t.Execute(os.Stdout, vFile.BlockAllocationTable.BAT[startRange:endRange+1])
	} else {
		nonEmptyBATEntries := make(map[int]uint32)
		for blockIndex := startRange; blockIndex <= endRange; blockIndex++ {
			if vFile.BlockAllocationTable.HasData(blockIndex) {
				nonEmptyBATEntries[int(blockIndex-startRange)] = vFile.BlockAllocationTable.BAT[blockIndex]
			}
		}

		t.Execute(os.Stdout, nonEmptyBATEntries)
	}

	return nil
}

const fixedDiskBlockInfoTempl = `Block sector size : 512 bytes
Block size        : {{.BlockSize}} bytes
Total blocks      : {{.BlockCount}}
`

const expandableDiskBlockInfoTempl = `Block sector size                  : 512 bytes
Block data section size            : {{.BlockDataSize}} bytes
Block bitmap section size          : {{.BlockBitmapSize}} bytes
Block bitmap section size (padded) : {{.BlockBitmapPaddedSize}} bytes
Total blocks                       : {{.BlockCount}} (Used: {{.UsedBlockCount}} Empty: {{.EmptyBlockCount}})
`

func showVhdBlocksInfo(c *cli.Context) error {
	vhdPath := c.String("path")
	if vhdPath == "" {
		return errors.New("Missing required argument --path")
	}

	vFileFactory := &vhdFile.FileFactory{}
	vFile, err := vFileFactory.Create(vhdPath)
	if err != nil {
		panic(err)
	}
	defer vFileFactory.Dispose(nil)

	vBlockFactory, err := vFile.GetBlockFactory()
	if err != nil {
		return err
	}

	if vFile.GetDiskType() == footer.DiskTypeFixed {
		info := &FixedDiskBlocksInfo{
			BlockSize:  vBlockFactory.GetBlockSize(),
			BlockCount: vBlockFactory.GetBlockCount(),
		}
		// Note: Identifying empty and used blocks of a FixedDisk requires reading each
		// block and checking it contains all zeros, which is time consuming so we don't
		// show those information.
		t, err := template.New("root").
			Parse(fixedDiskBlockInfoTempl)
		if err != nil {
			return err
		}
		t.Execute(os.Stdout, info)
	} else {
		info := &ExpandableDiskBlocksInfo{
			BlockDataSize:         vBlockFactory.GetBlockSize(),
			BlockBitmapSize:       vFile.BlockAllocationTable.GetBitmapSizeInBytes(),
			BlockBitmapPaddedSize: vFile.BlockAllocationTable.GetSectorPaddedBitmapSizeInBytes(),
			BlockCount:            vBlockFactory.GetBlockCount(),
		}

		for _, v := range vFile.BlockAllocationTable.BAT {
			if v == vhdcore.VhdNoDataInt {
				info.EmptyBlockCount++
			} else {
				info.UsedBlockCount++
			}
		}

		t, err := template.New("root").
			Parse(expandableDiskBlockInfoTempl)
		if err != nil {
			return err
		}
		t.Execute(os.Stdout, info)
	}

	return nil
}

func showVhdBlockBitmap(c *cli.Context) error {
	const bytesPerLine int32 = 8
	const bitsPerLine int32 = 8 * bytesPerLine

	vhdPath := c.String("path")
	if vhdPath == "" {
		return errors.New("Missing required argument --path")
	}

	if !c.IsSet("block-index") {
		return errors.New("Missing required argument --block-index")
	}

	blockIndex := uint32(0)
	id, err := strconv.ParseUint(c.String("block-index"), 10, 32)
	if err != nil {
		return fmt.Errorf("invalid index value --block-index: %s\n", err)
	}
	blockIndex = uint32(id)

	vFileFactory := &vhdFile.FileFactory{}
	vFile, err := vFileFactory.Create(vhdPath)
	if err != nil {
		return err
	}
	defer vFileFactory.Dispose(nil)

	if vFile.GetDiskType() == footer.DiskTypeFixed {
		return errors.New("Warn: Only expandable VHDs has bitmap associated with blocks, this is a fixed VHD")
	}

	vBlockFactory, err := vFile.GetBlockFactory()
	if err != nil {
		return err
	}

	if int64(blockIndex) > vBlockFactory.GetBlockCount()-1 {
		return fmt.Errorf("Warn: given block index %d is out of boundary, block index range is [0 : %d]", blockIndex, vBlockFactory.GetBlockCount()-1)
	}

	vBlock, err := vBlockFactory.Create(blockIndex)
	if err != nil {
		return err
	}

	if vBlock.IsEmpty {
		fmt.Print("The block that this bitmap belongs to is marked as empty\n\n")
		fmt.Print(createEmptyBitmapString(bytesPerLine, bitsPerLine, vFile.BlockAllocationTable.GetBitmapSizeInBytes()))
		return nil
	}

	fmt.Print(createBitmapString(bitsPerLine, vBlock.BitMap))
	return nil
}

func createEmptyBitmapString(bytesPerLine, bitsPerLine, bitmapSizeInBytes int32) string {
	var buffer bytes.Buffer
	line := ""
	for i := int32(0); i < bytesPerLine; i++ {
		line = line + " " + "00000000"
	}

	count := bitmapSizeInBytes / bytesPerLine
	pad := len(strconv.FormatInt(int64(bitmapSizeInBytes*8), 10))
	fmtLine := fmt.Sprintf("[%%-%dd - %%%dd]", pad, pad)
	for i := int32(0); i < count; i++ {
		buffer.WriteString(fmt.Sprintf(fmtLine, i*bitsPerLine, i*bitsPerLine+bitsPerLine-1))
		buffer.WriteString(line)
		buffer.WriteString("\n")
	}

	remaining := bitmapSizeInBytes % bytesPerLine
	if remaining != 0 {
		buffer.WriteString(fmt.Sprintf(fmtLine, count*bitsPerLine, count*bitsPerLine+8*remaining-1))
		for i := int32(0); i < remaining; i++ {
			buffer.WriteString(" 00000000")
		}
	}

	buffer.WriteString("\n")
	return buffer.String()
}

func createBitmapString(bitsPerLine int32, vBlockBitmap *bitmap.BitMap) string {
	var buffer bytes.Buffer
	pad := len(strconv.FormatInt(int64(vBlockBitmap.Length), 10))
	fmtLine := fmt.Sprintf("[%%-%dd - %%%dd]", pad, pad)
	for i := int32(0); i < vBlockBitmap.Length; {
		if i%bitsPerLine == 0 {
			if i < vBlockBitmap.Length-bitsPerLine {
				buffer.WriteString(fmt.Sprintf(fmtLine, i, i+bitsPerLine-1))
			} else {
				buffer.WriteString(fmt.Sprintf(fmtLine, i, vBlockBitmap.Length-1))
			}
		}

		b := byte(0)
		for j := uint32(0); j < 8; j++ {
			if dirty, _ := vBlockBitmap.Get(i); dirty {
				b |= byte(1 << (7 - j))
			}
			i++
		}
		buffer.WriteByte(' ')
		buffer.WriteString(fmt.Sprintf("%08b", b))
		if i%bitsPerLine == 0 {
			buffer.WriteString("\n")
		}
	}
	buffer.WriteString("\n")
	return buffer.String()
}
