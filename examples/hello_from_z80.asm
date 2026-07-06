; hello_from_z80.asm — the Phase 1 end-to-end example.
;
; Copies the message into RAM at msg_dst using a classic load/store
; loop, then halts. Run it with:
;
;   pasmo examples/hello_from_z80.asm build/hello_from_z80.bin
;   go run ./cmd/zrun -org 0x8000 build/hello_from_z80.bin
;
; The RAM dump must match examples/hello_from_z80.golden.

        org 8000h

msg_dst equ 9000h

start:
        ld hl, message
        ld de, msg_dst
        ld b, msg_len
copy:
        ld a, (hl)
        ld (de), a
        inc hl
        inc de
        djnz copy
        halt

message:
        db "HELLO FROM Z80"
msg_len equ $ - message

        end start
