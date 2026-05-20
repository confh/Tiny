package vm

type IncLocalInfo struct {
	Slot   int
	Amount int
}

func OptimizeBytecode(instructions []Instruction) []Instruction {
	instructions = optimizeIncLocal(instructions)
	instructions = optimizeJumpLocalGEConst(instructions)
	instructions = optimizeAddAssignLocal(instructions)
	instructions = optimizeSubtractAssignLocal(instructions)
	instructions = optimizeJumpLocalGELocal(instructions)
	instructions = optimizeJumpModLocalLocalNotZero(instructions)
	// instructions = removeDeadCodeAfterReturn(instructions)

	return instructions
}

func optimizeJumpModLocalLocalNotZero(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// Pattern:
		// OP_LOAD_LOCAL left
		// OP_LOAD_LOCAL right
		// OP_MOD
		// OP_CONST 0
		// OP_EQ
		// OP_JUMP_IF_FALSE target
		if i+5 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_MOD &&
			instructions[i+3].Op == OP_CONST &&
			instructions[i+4].Op == OP_EQ &&
			instructions[i+5].Op == OP_JUMP_IF_FALSE {

			leftSlot, leftOK := instructions[i].Value.(int)
			rightSlot, rightOK := instructions[i+1].Value.(int)
			zero, zeroOK := constIntAmount(instructions[i+3].Value)
			target, targetOK := instructions[i+5].Value.(int)

			if leftOK && rightOK && zeroOK && zero == 0 && targetOK {
				optimized = append(optimized, Instruction{
					Op: OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO,
					Value: JumpModLocalLocalNotZeroInfo{
						LeftSlot:  leftSlot,
						RightSlot: rightSlot,
						Target:    target,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex
				oldToNew[i+4] = newIndex
				oldToNew[i+5] = newIndex

				i += 6
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeAddAssignLocal(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))

	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// Pattern:
		// OP_LOAD_LOCAL target
		// OP_LOAD_LOCAL source
		// OP_ADD
		// OP_ASSIGN_LOCAL target
		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_ADD &&
			instructions[i+3].Op == OP_ASSIGN_LOCAL {

			targetLoadSlot, targetLoadOK := instructions[i].Value.(int)
			sourceSlot, sourceOK := instructions[i+1].Value.(int)
			assignSlot, assignOK := instructions[i+3].Value.(int)

			if targetLoadOK && sourceOK && assignOK && targetLoadSlot == assignSlot {
				optimized = append(optimized, Instruction{
					Op: OP_ADD_ASSIGN_LOCAL,
					Value: AssignLocalInfo{
						TargetSlot: targetLoadSlot,
						SourceSlot: sourceSlot,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeSubtractAssignLocal(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))

	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// Pattern:
		// OP_LOAD_LOCAL target
		// OP_LOAD_LOCAL source
		// OP_ADD
		// OP_ASSIGN_LOCAL target
		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_SUB &&
			instructions[i+3].Op == OP_ASSIGN_LOCAL {

			targetLoadSlot, targetLoadOK := instructions[i].Value.(int)
			sourceSlot, sourceOK := instructions[i+1].Value.(int)
			assignSlot, assignOK := instructions[i+3].Value.(int)

			if targetLoadOK && sourceOK && assignOK && targetLoadSlot == assignSlot {
				optimized = append(optimized, Instruction{
					Op: OP_SUB_ASSIGN_LOCAL,
					Value: AssignLocalInfo{
						TargetSlot: targetLoadSlot,
						SourceSlot: sourceSlot,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeJumpLocalGEConst(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))

	// old instruction index -> new instruction index
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// Pattern:
		// OP_LOAD_LOCAL slot
		// OP_CONST int
		// OP_LT
		// OP_JUMP_IF_FALSE target
		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_CONST &&
			instructions[i+2].Op == OP_LT &&
			instructions[i+3].Op == OP_JUMP_IF_FALSE {

			slot, slotOK := instructions[i].Value.(int)
			constValue, constOK := constIntAmount(instructions[i+1].Value)
			target, targetOK := instructions[i+3].Value.(int)

			if slotOK && constOK && targetOK {
				optimized = append(optimized, Instruction{
					Op: OP_JUMP_LOCAL_GE_CONST,
					Value: JumpLocalGEConstInfo{
						Slot:   slot,
						Value:  constValue,
						Target: target, // old target for now, remapped below
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeJumpLocalGELocal(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_LT &&
			instructions[i+3].Op == OP_JUMP_IF_FALSE {

			leftSlot, leftOK := instructions[i].Value.(int)
			rightSlot, rightOK := instructions[i+1].Value.(int)
			target, targetOK := instructions[i+3].Value.(int)

			if leftOK && rightOK && targetOK {
				optimized = append(optimized, Instruction{
					Op: OP_JUMP_LOCAL_GE_LOCAL,
					Value: JumpLocalGELocalInfo{
						LeftSlot:  leftSlot,
						RightSlot: rightSlot,
						Target:    target,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func remapJumpTargets(instructions []Instruction, oldToNew []int) {
	for i := range instructions {
		switch instructions[i].Op {
		case OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
			info := instructions[i].Value.(JumpModLocalLocalNotZeroInfo)

			if info.Target >= 0 && info.Target < len(oldToNew) {
				info.Target = oldToNew[info.Target]
			}

			instructions[i].Value = info

		case OP_JUMP_LOCAL_GE_LOCAL:
			info := instructions[i].Value.(JumpLocalGELocalInfo)

			if info.Target >= 0 && info.Target < len(oldToNew) {
				info.Target = oldToNew[info.Target]
			}

			instructions[i].Value = info

		case OP_JUMP, OP_JUMP_IF_FALSE:
			target, ok := instructions[i].Value.(int)
			if ok && target >= 0 && target < len(oldToNew) {
				instructions[i].Value = oldToNew[target]
			}

		case OP_JUMP_LOCAL_GE_CONST:
			info := instructions[i].Value.(JumpLocalGEConstInfo)

			if info.Target >= 0 && info.Target < len(oldToNew) {
				info.Target = oldToNew[info.Target]
			}

			instructions[i].Value = info

		case OP_SETUP_TRY:
			info, ok := instructions[i].Value.(TryInfo)
			if ok {
				if info.CatchIP >= 0 && info.CatchIP < len(oldToNew) {
					info.CatchIP = oldToNew[info.CatchIP]
				}

				instructions[i].Value = info
			}
		}
	}
}

func optimizeIncLocal(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))

	for i := 0; i < len(instructions); {
		// Pattern:
		// OP_LOAD_LOCAL slot
		// OP_CONST amount
		// OP_ADD
		// OP_ASSIGN_LOCAL same slot
		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_CONST &&
			instructions[i+2].Op == OP_ADD &&
			instructions[i+3].Op == OP_ASSIGN_LOCAL {

			loadSlot, loadOK := instructions[i].Value.(int)
			assignSlot, assignOK := instructions[i+3].Value.(int)

			amount, amountOK := constIntAmount(instructions[i+1].Value)

			if loadOK && assignOK && amountOK && loadSlot == assignSlot {
				optimized = append(optimized, Instruction{
					Op: OP_INC_LOCAL,
					Value: IncLocalInfo{
						Slot:   loadSlot,
						Amount: amount,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		i++
	}

	return optimized
}

func constIntAmount(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v == float64(int(v)) {
			return int(v), true
		}
		return 0, false
	case float32:
		if v == float32(int(v)) {
			return int(v), true
		}
		return 0, false
	default:
		return 0, false
	}
}

func removeDeadCodeAfterReturn(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	dead := false

	for _, instr := range instructions {
		if dead {
			// Stop removing when we hit a jump target-ish instruction.
			// For now, keep this conservative.
			if instr.Op == OP_SETUP_TRY {
				dead = false
			} else {
				continue
			}
		}

		optimized = append(optimized, instr)

		if instr.Op == OP_RETURN {
			dead = true
		}
	}

	return optimized
}
