// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IUniswapV2Router {
    function factory() external pure returns (address);
    function WEth() external pure returns (address);

    function addLiquidityEth(
        address token,
        uint amountTokenDesired,
        uint amountTokenMin,
        uint amountEthMin,
        address to,
        uint deadline
    ) external payable returns (uint amountToken, uint amountEth, uint liquidity);
    
    function removeLiquidityEth(
        address token,
        uint liquidity,
        uint amountTokenMin,
        uint amountEthMin,
        address to,
        uint deadline
    ) external returns (uint amountToken, uint amountEth);
    
    function swapExactTokensForEthSupportingFeeOnTransferTokens(
        uint amountIn,
        uint amountOutMin,
        address[] calldata path,
        address to,
        uint deadline
    ) external;
}

interface IUniswapV2Factory {
    function createPair(address tokenA, address tokenB) external returns (address pair);
}

interface IERC20 {
    function balanceOf(address account) external view returns (uint256);
    function transfer(address recipient, uint256 amount) external returns (bool);
    function approve(address spender, uint256 amount) external returns (bool);
    function allowance(address owner, address spender) external view returns (uint256);
}

contract MemeToken {
    string public name;
    string public symbol;
    uint8 public decimals;
    uint256 public totalSupply;
    
    uint256 public taxRate = 1000; // 10%税率
    uint256 constant TAX_BASE = 10000; // 税率基数100%
    
    uint256 public rewardShare = 5000; // 50%
    uint256 public liquidityShare = 5000; // 50%
    
    uint256 public maxTxAmount; // 最大交易金额
    uint256 public dailyTxLimit; // 每日交易次数限制
    mapping(address => uint256) public lastTxTime;
    mapping(address => uint256) public dailyTxCount;
    mapping(address => uint256) public lastTxDate;
    
    mapping(address => bool) public isExcludedFromFee;
    
    address public owner;
    address public liquidityWallet;
    address public uniswapV2Pair;
    IUniswapV2Router public uniswapV2Router;
    
    mapping(address => uint256) private _balances;
    mapping(address => mapping(address => uint256)) private _allowances;
    
    mapping(address => bool) public blacklisted;
    
    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);
    event TaxDistributed(uint256 rewardAmount, uint256 liquidityAmount);
    event LiquidityAdded(uint256 tokenAmount, uint256 ethAmount);
    
    modifier onlyOwner() {
        require(msg.sender == owner, "You are not the owner");
        _;
    }
    
    modifier notBlacklisted(address account) {
        require(!blacklisted[account], "Address is blacklisted");
        _;
    }
    
    constructor(
        string memory _name,
        string memory _symbol,
        uint256 _totalSupply,
        address _router
    ) {
        name = _name;
        symbol = _symbol;
        decimals = 18;
        totalSupply = _totalSupply * 10**decimals;
        owner = msg.sender;
        liquidityWallet = msg.sender;
        
        uniswapV2Router = IUniswapV2Router(_router);
        uniswapV2Pair = IUniswapV2Factory(uniswapV2Router.factory()).createPair(
            address(this),
            uniswapV2Router.WEth()
        );
        
        maxTxAmount = totalSupply / 100;
        dailyTxLimit = 3;
        
        isExcludedFromFee[owner] = true;
        isExcludedFromFee[address(this)] = true;
        isExcludedFromFee[uniswapV2Pair] = true;
        
        _balances[msg.sender] = totalSupply;
        emit Transfer(address(0), msg.sender, totalSupply);
    }
    
    function balanceOf(address account) public view returns (uint256) {
        return _balances[account];
    }
    
    function transfer(address recipient, uint256 amount) 
        public 
        notBlacklisted(msg.sender)
        notBlacklisted(recipient)
        returns (bool) 
    {
        _checkTxLimits(msg.sender, amount);
        _updateTxCount(msg.sender);
        
        // 是否豁免税费
        bool takeFee = true;
        if (isExcludedFromFee[msg.sender] || isExcludedFromFee[recipient]) {
            takeFee = false;
        }
        
        uint256 taxAmount = takeFee ? _calculateTax(amount) : 0;
        uint256 transferAmount = amount - taxAmount;
        
        // 扣除发送方余额
        _balances[msg.sender] -= amount;
        
        // 分配税费
        if (taxAmount > 0) {
            _distributeTax(taxAmount);
        }
        
        // 转账给接收方
        _balances[recipient] += transferAmount;
        
        emit Transfer(msg.sender, recipient, transferAmount);
        if (taxAmount > 0) {
            emit Transfer(msg.sender, address(this), taxAmount);
        }
        
        return true;
    }
    
    function _checkTxLimits(address sender, uint256 amount) internal view {
        require(amount <= maxTxAmount, "Exceeds maximum transaction amount");
        
        uint256 today = block.timestamp / 1 days;
        if (lastTxDate[sender] == today) {
            require(dailyTxCount[sender] < dailyTxLimit, "Exceeds daily transaction limit");
        }
    }
    
    function _updateTxCount(address sender) internal {
        uint256 today = block.timestamp / 1 days;
        
        if (lastTxDate[sender] != today) {
            dailyTxCount[sender] = 0;
            lastTxDate[sender] = today;
        }
        
        dailyTxCount[sender]++;
        lastTxTime[sender] = block.timestamp;
    }
    
    function _calculateTax(uint256 amount) internal view returns (uint256) {
        return (amount * taxRate) / TAX_BASE;
    }
    
    function _distributeTax(uint256 taxAmount) internal {
        uint256 rewardAmount = (taxAmount * rewardShare) / TAX_BASE;
        uint256 liquidityAmount = taxAmount - rewardAmount;
        
        // 持币者奖励
        if (rewardAmount > 0) {
            _balances[address(this)] += rewardAmount;
        }
        
        if (liquidityAmount > 0) {
            _balances[address(this)] += liquidityAmount;
        }

        _processLiquidity();
        
        emit TaxDistributed(rewardAmount, liquidityAmount);
    }
    
    function _processLiquidity() view internal {
        uint256 contractTokenBalance = _balances[address(this)];
        uint256 minTokensForLiquidity = totalSupply / 10000;
        
        if (contractTokenBalance >= minTokensForLiquidity) {
        }
    }
    
    function manualAddLiquidity(uint256 tokenAmount) external onlyOwner {
        require(tokenAmount <= _balances[address(this)], "Insufficient contract balance");
        _approve(address(this), address(uniswapV2Router), tokenAmount);
        
        // 添加流动性
        uniswapV2Router.addLiquidityEth{value: address(this).balance}(
            address(this),
            tokenAmount,
            0,
            0,
            liquidityWallet,
            block.timestamp
        );
        
        emit LiquidityAdded(tokenAmount, address(this).balance);
    }
    
    function approve(address spender, uint256 amount) public returns (bool) {
        _approve(msg.sender, spender, amount);
        return true;
    }
    
    function _approve(address sender, address spender, uint256 amount) internal {
        _allowances[sender][spender] = amount;
        emit Approval(sender, spender, amount);
    }
    
    function allowance(address _owner, address spender) public view returns (uint256) {
        return _allowances[_owner][spender];
    }
    
    function transferFrom(address sender, address recipient, uint256 amount) 
        public 
        notBlacklisted(sender)
        notBlacklisted(recipient)
        returns (bool) 
    {
        _checkTxLimits(sender, amount);
        _updateTxCount(sender);
        
        uint256 currentAllowance = _allowances[sender][msg.sender];
        require(currentAllowance >= amount, "Transfer amount exceeds allowance");
        
        // 是否豁免税费
        bool takeFee = true;
        if (isExcludedFromFee[sender] || isExcludedFromFee[recipient]) {
            takeFee = false;
        }
        
        uint256 taxAmount = takeFee ? _calculateTax(amount) : 0;
        uint256 transferAmount = amount - taxAmount;
        
        // 扣除发送方余额和额度
        _balances[sender] -= amount;
        _allowances[sender][msg.sender] = currentAllowance - amount;
        
        if (taxAmount > 0) {
            _distributeTax(taxAmount);
        }
        
        // 转账给接收方
        _balances[recipient] += transferAmount;
        
        emit Transfer(sender, recipient, transferAmount);
        if (taxAmount > 0) {
            emit Transfer(sender, address(this), taxAmount);
        }
        
        return true;
    }
    
    // 管理员相关
    function setTaxRate(uint256 newTaxRate) public onlyOwner {
        require(newTaxRate <= 2000, "Tax rate cannot exceed 20%");
        taxRate = newTaxRate;
    }
    
    function setTaxDistribution(uint256 newRewardShare, uint256 newLiquidityShare) public onlyOwner {
        require(newRewardShare + newLiquidityShare == TAX_BASE, "Shares must sum to 100%");
        rewardShare = newRewardShare;
        liquidityShare = newLiquidityShare;
    }
    
    function setTxLimits(uint256 newMaxTxAmount, uint256 newDailyTxLimit) public onlyOwner {
        require(newMaxTxAmount >= totalSupply / 1000, "Max transaction amount too low");
        require(newDailyTxLimit <= 10, "Daily transaction limit too high");
        
        maxTxAmount = newMaxTxAmount;
        dailyTxLimit = newDailyTxLimit;
    }
    
    function setBlacklist(address account, bool isBlacklisted) public onlyOwner {
        blacklisted[account] = isBlacklisted;
    }
    
    function setExcludedFromFee(address account, bool excluded) public onlyOwner {
        isExcludedFromFee[account] = excluded;
    }
    
    function rescueTokens(address tokenAddress) public onlyOwner {
        IERC20 token = IERC20(tokenAddress);
        uint256 balance = token.balanceOf(address(this));
        require(token.transfer(owner, balance), "Token transfer failed");
    }
    
    function rescueEth() public onlyOwner {
        payable(owner).transfer(address(this).balance);
    }
    
    receive() external payable {}
}