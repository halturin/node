package etf

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
)

// linked list for decoding complex types like list/map/tuple
type stackElement struct {
	parent *stackElement

	termType byte

	term     Term //value
	i        int  // current
	children int
	tmp      Term // temporary value. uses as a temporary storage for a key of map
}

var (
	termNil = make(List, 0)

	biggestInt = big.NewInt(0xfffffffffffffff)
	lowestInt  = big.NewInt(-0xfffffffffffffff)

	ErrMalformedAtomUTF8      = fmt.Errorf("Malformed ETF. ettAtomUTF8")
	ErrMalformedSmallAtomUTF8 = fmt.Errorf("Malformed ETF. ettSmallAtomUTF8")
	ErrMalformedString        = fmt.Errorf("Malformed ETF. ettString")
	ErrMalformedCacheRef      = fmt.Errorf("Malformed ETF. ettCacheRef")
	ErrMalformedNewFloat      = fmt.Errorf("Malformed ETF. ettNewFloat")
	ErrMalformedSmallInteger  = fmt.Errorf("Malformed ETF. ettSmallInteger")
	ErrMalformedInteger       = fmt.Errorf("Malformed ETF. ettInteger")
	ErrMalformedSmallBig      = fmt.Errorf("Malformed ETF. ettSmallBig")
	ErrMalformedLargeBig      = fmt.Errorf("Malformed ETF. ettLargeBig")
	ErrMalformedList          = fmt.Errorf("Malformed ETF. ettList")
	ErrMalformedSmallTuple    = fmt.Errorf("Malformed ETF. ettSmallTuple")
	ErrMalformedLargeTuple    = fmt.Errorf("Malformed ETF. ettLargeTuple")
	ErrMalformedMap           = fmt.Errorf("Malformed ETF. ettMap")
	ErrMalformedBinary        = fmt.Errorf("Malformed ETF. ettBinary")
	ErrMalformedBitBinary     = fmt.Errorf("Malformed ETF. ettBitBinary")
	ErrMalformedPid           = fmt.Errorf("Malformed ETF. ettPid")
	ErrMalformedNewPid        = fmt.Errorf("Malformed ETF. ettNewPid")
	ErrMalformedRef           = fmt.Errorf("Malformed ETF. ettNewRef")
	ErrMalformedNewRef        = fmt.Errorf("Malformed ETF. ettNewerRef")
	ErrMalformedPort          = fmt.Errorf("Malformed ETF. ettPort")
	ErrMalformedNewPort       = fmt.Errorf("Malformed ETF. ettNewPort")
	ErrMalformedUnknownType   = fmt.Errorf("Malformed ETF. unknown type")
	ErrMalformedFun           = fmt.Errorf("Malformed ETF. ettNewFun")
	ErrMalformedPacketLength  = fmt.Errorf("Malformed ETF. incorrect length of packet")
	ErrMalformed              = fmt.Errorf("Malformed ETF")
	ErrInternal               = fmt.Errorf("Internal error")
)

// using iterative way is speeding up it up to x25 times
// so this implementation has no recursion calls at all

// it might looks super hard to understand the logic, but
// there are only two stages
// 1) Stage1: decoding basic types (long list of type we have to support)
// 2) Stage2: decoding list/tuples/maps and complex types like Port/Pid/Ref using stack
//
// see comments within this function

func Decode(packet []byte, cache []Atom) (Term, error) {
	var term Term
	var stack *stackElement
	var child *stackElement
	var t byte

	for {
		child = nil
		if len(packet) == 0 {
			return nil, ErrMalformed
		}

		t = packet[0]
		packet = packet[1:]

		// Stage 1: decoding base type. if have encountered List/Map/Tuple
		// or complex type like Pid/Ref/Port:
		//  save the state in stackElement and push it to the stack (basically,
		//  we just append the new item to the linked list)
		//

		switch t {
		case ettAtomUTF8, ettAtom:
			if len(packet) < 2 {
				return nil, ErrMalformedAtomUTF8
			}

			n := binary.BigEndian.Uint16(packet)
			if len(packet) < int(n+2) {
				return nil, ErrMalformedAtomUTF8
			}

			term = Atom(packet[2 : n+2])
			packet = packet[n+2:]

		case ettSmallAtomUTF8, ettSmallAtom:
			if len(packet) == 0 {
				return nil, ErrMalformedSmallAtomUTF8
			}

			n := int(packet[0])
			if len(packet) < n+1 {
				return nil, ErrMalformedSmallAtomUTF8
			}

			term = Atom(packet[1 : n+1])
			packet = packet[n+1:]

		case ettString:
			if len(packet) < 2 {
				return nil, ErrMalformedString
			}

			n := binary.BigEndian.Uint16(packet)
			if len(packet) < int(n+2) {
				return nil, ErrMalformedString
			}

			term = string(packet[2 : n+2])
			packet = packet[n+2:]

		case ettCacheRef:
			if len(packet) == 0 {
				return nil, ErrMalformedCacheRef
			}
			term = cache[int(packet[0])]
			packet = packet[1:]

		case ettNewFloat:
			if len(packet) < 8 {
				return nil, ErrMalformedNewFloat
			}
			bits := binary.BigEndian.Uint64(packet[:8])

			term = math.Float64frombits(bits)
			packet = packet[8:]

		case ettSmallInteger:
			if len(packet) == 0 {
				return nil, ErrMalformedSmallInteger
			}

			term = int(packet[0])
			packet = packet[1:]

		case ettInteger:
			if len(packet) < 4 {
				return nil, ErrMalformedInteger
			}

			term = int64(int32(binary.BigEndian.Uint32(packet[:4])))
			packet = packet[4:]

		case ettSmallBig:
			if len(packet) == 0 {
				return nil, ErrMalformedSmallBig
			}

			n := packet[0]
			negative := packet[1] == 1 // sign

			///// this block improve the performance at least 4 times
			// see details in benchmarks
			if n < 8 { // treat as an int64
				le8 := make([]byte, 8)
				copy(le8, packet[2:n+2])
				smallBig := binary.LittleEndian.Uint64(le8)
				if negative {
					smallBig = -smallBig
				}

				term = int64(smallBig)
				packet = packet[n+2:]
				break
			}
			/////

			if len(packet) < int(n+2) {
				return nil, ErrMalformedSmallBig
			}
			bytes := packet[2 : n+2]

			// encoded as a little endian. convert it to the big endian order
			l := len(bytes)
			for i := 0; i < l/2; i++ {
				bytes[i], bytes[l-1-i] = bytes[l-1-i], bytes[i]
			}

			bigInt := &big.Int{}
			bigInt.SetBytes(bytes)
			if negative {
				bigInt = bigInt.Neg(bigInt)
			}

			// try int and int64
			if bigInt.Cmp(biggestInt) < 0 && bigInt.Cmp(lowestInt) > 0 {
				term = bigInt.Int64()
				packet = packet[n+2:]
				break
			}

			term = bigInt
			packet = packet[n+2:]

		case ettLargeBig:
			if len(packet) < 256 { // must be longer than ettSmallBig
				return nil, ErrMalformedLargeBig
			}

			n := binary.BigEndian.Uint32(packet[:4])
			negative := packet[4] == 1 // sign

			if len(packet) < int(n+5) {
				return nil, ErrMalformedLargeBig
			}
			bytes := packet[5 : n+5]

			// encoded as a little endian. convert it to the big endian order
			l := len(bytes)
			for i := 0; i < l/2; i++ {
				bytes[i], bytes[l-1-i] = bytes[l-1-i], bytes[i]
			}

			bigInt := &big.Int{}
			bigInt.SetBytes(bytes)
			if negative {
				bigInt = bigInt.Neg(bigInt)
			}

			term = bigInt
			packet = packet[n+5:]

		case ettList:
			if len(packet) < 4 {
				return nil, ErrMalformedList
			}

			n := binary.BigEndian.Uint32(packet[:4])
			if n == 0 {
				// must be encoded as ettNil
				return nil, ErrMalformedList
			}

			term = make(List, n+1)
			packet = packet[4:]
			child = &stackElement{
				parent:   stack,
				termType: ettList,
				term:     term,
				children: int(n + 1),
			}

		case ettSmallTuple:
			if len(packet) == 0 {
				return nil, ErrMalformedSmallTuple
			}

			n := packet[0]
			packet = packet[1:]
			term = make(Tuple, n)

			if n == 0 {
				break
			}

			child = &stackElement{
				parent:   stack,
				termType: ettSmallTuple,
				term:     term,
				children: int(n),
			}

		case ettLargeTuple:
			if len(packet) < 4 {
				return nil, ErrMalformedLargeTuple
			}

			n := binary.BigEndian.Uint32(packet[:4])
			packet = packet[4:]
			term = make(Tuple, n)

			if n == 0 {
				break
			}

			child = &stackElement{
				parent:   stack,
				termType: ettLargeTuple,
				term:     term,
				children: int(n),
			}

		case ettMap:
			if len(packet) < 4 {
				return nil, ErrMalformedMap
			}

			n := binary.BigEndian.Uint32(packet[:4])
			packet = packet[4:]
			term = make(Map)

			if n == 0 {
				break
			}

			child = &stackElement{
				parent:   stack,
				termType: ettMap,
				term:     term,
				children: int(n) * 2,
			}

		case ettBinary:
			if len(packet) < 4 {
				return nil, ErrMalformedBinary
			}

			n := binary.BigEndian.Uint32(packet)
			if len(packet) < int(n+4) {
				return nil, ErrMalformedBinary
			}

			b := make([]byte, n)
			copy(b, packet[4:n+4])

			term = b
			packet = packet[n+4:]

		case ettNil:
			term = termNil

		case ettPid, ettNewPid:
			child = &stackElement{
				parent:   stack,
				termType: t,
				children: 1,
			}

		case ettNewRef, ettNewerRef:
			if len(packet) < 2 {
				return nil, ErrMalformedRef
			}

			l := binary.BigEndian.Uint16(packet[:2])
			packet = packet[2:]

			child = &stackElement{
				parent:   stack,
				termType: t,
				children: 1,
				tmp:      l, // save length in temporary place of the stack element
			}

			//case ettExport:
		case ettNewFun:
			var unique [16]byte

			if len(packet) < 32 {
				return nil, ErrMalformedFun
			}

			copy(unique[:], packet[5:21])
			l := binary.BigEndian.Uint32(packet[25:29])

			fun := Function{
				Arity:    packet[4],
				Unique:   unique,
				Index:    binary.BigEndian.Uint32(packet[21:25]),
				FreeVars: make([]Term, l),
			}

			child = &stackElement{
				parent:   stack,
				termType: t,
				term:     fun,
				children: 4 + int(l),
			}
			packet = packet[29:]

		case ettPort, ettNewPort:
			child = &stackElement{
				parent:   stack,
				termType: t,
				children: 1,
			}

		case ettBitBinary:
			if len(packet) < 6 {
				return nil, ErrMalformedBitBinary
			}

			n := binary.BigEndian.Uint32(packet)
			bits := uint(packet[4])

			b := make([]byte, n)
			copy(b, packet[5:n+5])
			b[n-1] = b[n-1] >> (8 - bits)

			term = b
			packet = packet[n+5:]

		default:
			term = nil
			return nil, ErrMalformedUnknownType
		}

		// it was a single element
		if stack == nil && child == nil {
			break
		}

		// decoded child item is List/Map/Tuple/Pid/Ref/Port/... going deeper
		if child != nil {
			stack = child
			continue
		}

		// Stage 2
	processStack:
		if stack != nil {
			switch stack.termType {
			case ettList:
				stack.term.(List)[stack.i] = term
				stack.i++
				// remove the last element for proper list (its ettNil)
				if stack.i == stack.children && t == ettNil {
					stack.term = stack.term.(List)[:stack.i-1]
				}

			case ettSmallTuple, ettLargeTuple:
				stack.term.(Tuple)[stack.i] = term
				stack.i++

			case ettMap:
				if stack.i&0x01 == 0x01 { // value
					stack.term.(Map)[stack.tmp] = term
					stack.i++
					break
				}

				// a key
				stack.tmp = term
				stack.i++

			case ettPid:
				if len(packet) < 9 {
					return nil, ErrMalformedPid
				}

				name, ok := term.(Atom)
				if !ok {
					return nil, ErrMalformedPid
				}

				pid := Pid{
					Node:     name,
					Id:       binary.BigEndian.Uint32(packet[:4]),
					Serial:   binary.BigEndian.Uint32(packet[4:8]),
					Creation: packet[8] & 3, // only two bits are significant, rest are to be 0
				}

				packet = packet[9:]
				stack.term = pid
				stack.i++

			case ettNewPid:
				if len(packet) < 12 {
					return nil, ErrMalformedNewPid
				}

				name, ok := term.(Atom)
				if !ok {
					return nil, ErrMalformedPid
				}

				pid := Pid{
					Node:   name,
					Id:     binary.BigEndian.Uint32(packet[:4]),
					Serial: binary.BigEndian.Uint32(packet[4:8]),
					// FIXME: we must upgrade this type to uint32
					// Creation: binary.BigEndian.Uint32(packet[8:12])
					Creation: packet[11], // use the last byte for a while
				}

				packet = packet[12:]
				stack.term = pid
				stack.i++

			case ettNewRef:
				var id uint32
				name, ok := term.(Atom)
				if !ok {
					return nil, ErrMalformedRef
				}

				l := stack.tmp.(uint16)
				stack.tmp = nil
				expectedLength := int(1 + l*4)

				if len(packet) < expectedLength {
					return nil, ErrMalformedRef
				}

				ref := Ref{
					Node:     name,
					Id:       make([]uint32, l),
					Creation: packet[0],
				}
				packet = packet[1:]

				for i := 0; i < int(l); i++ {
					id = binary.BigEndian.Uint32(packet[:4])
					ref.Id[i] = id
					packet = packet[4:]
				}

				stack.term = ref
				stack.i++

			case ettNewerRef:
				var id uint32
				name, ok := term.(Atom)
				if !ok {
					return nil, ErrMalformedRef
				}

				l := stack.tmp.(uint16)
				stack.tmp = nil
				expectedLength := int(4 + l*4)

				if len(packet) < expectedLength {
					return nil, ErrMalformedRef
				}

				ref := Ref{
					Node: name,
					Id:   make([]uint32, l),
					// FIXME: we must upgrade this type to uint32
					// Creation: binary.BigEndian.Uint32(packet[:4])
					Creation: packet[3],
				}
				packet = packet[4:]

				for i := 0; i < int(l); i++ {
					id = binary.BigEndian.Uint32(packet[:4])
					ref.Id[i] = id
					packet = packet[4:]
				}

				stack.term = ref
				stack.i++

			case ettPort:
				if len(packet) < 5 {
					return nil, ErrMalformedPort
				}

				name, ok := term.(Atom)
				if !ok {
					return nil, ErrMalformedPort
				}

				port := Port{
					Node:     name,
					Id:       binary.BigEndian.Uint32(packet[:4]),
					Creation: packet[4],
				}

				packet = packet[5:]
				stack.term = port
				stack.i++

			case ettNewPort:
				if len(packet) < 8 {
					return nil, ErrMalformedNewPort
				}

				name, ok := term.(Atom)
				if !ok {
					return nil, ErrMalformedNewPort
				}

				port := Port{
					Node: name,
					Id:   binary.BigEndian.Uint32(packet[:4]),
					// FIXME: we must upgrade this type to uint32
					// Creation: binary.BigEndian.Uint32(packet[4:8])
					Creation: packet[7],
				}

				packet = packet[8:]
				stack.term = port
				stack.i++

			case ettNewFun:
				fun := stack.term.(Function)
				switch stack.i {
				case 0:
					// Module
					module, ok := term.(Atom)
					if !ok {
						return nil, ErrMalformedFun
					}
					fun.Module = module

				case 1:
					// OldIndex
					oldindex, ok := term.(int)
					if !ok {
						return nil, ErrMalformedFun
					}
					fun.OldIndex = uint32(oldindex)

				case 2:
					// OldUnique
					olduniq, ok := term.(int64)
					if !ok {
						return nil, ErrMalformedFun
					}
					fun.OldUnique = uint32(olduniq)

				case 3:
					// Pid
					pid, ok := term.(Pid)
					if !ok {
						return nil, ErrMalformedFun
					}
					fun.Pid = pid

				default:
					if len(fun.FreeVars) < (stack.i-4)+1 {
						return nil, ErrMalformedFun
					}
					fun.FreeVars[stack.i-4] = term
				}

				stack.term = fun
				stack.i++

			default:
				return nil, ErrInternal
			}
		}

		// we are still decoding children of Lis/Map/Tuple
		if stack.i < stack.children {
			continue
		}

		term = stack.term

		// this term was the last element of List/Map/Tuple (single level)
		// pop from the stack
		if stack.parent == nil {
			break
		}

		// stage 3: parent is List/Tuple/Map and we need to place
		// decoded term into the right place

		stack, stack.parent = stack.parent, nil // nil here is just a little help for GC
		goto processStack

	}

	// packet must have strict data length
	if len(packet) > 0 {
		return nil, ErrMalformedPacketLength
	}

	return term, nil
}

type Context struct{}
